package parser

import (
	"github.com/telemetryapp/gotelemetry"
	"github.com/telemetryapp/gotelemetry_agent/agent/aggregations"
	"sync"
	"testing"
)

type dummyNotificationProvider struct {
	notifications []gotelemetry.Notification
	channels      []string
}

func (d *dummyNotificationProvider) SendNotification(n gotelemetry.Notification, c string) bool {
	d.notifications = append(d.notifications, n)
	d.channels = append(d.channels, c)

	return true
}

func newDummyNotificationProvider() *dummyNotificationProvider {
	return &dummyNotificationProvider{[]gotelemetry.Notification{}, []string{}}
}

var parserTestInitOnce = sync.Once{}

func testRunAndReturnErrors(s string) (map[string]interface{}, *dummyNotificationProvider, []error) {
	np := newDummyNotificationProvider()

	parserTestInitOnce.Do(func() {
		l := "/tmp/agent.sqlite3"
		aggregations.Init(&l, make(chan error, 99999))
	})

	commands, errs := Parse("test", s)

	if len(errs) > 0 {
		return nil, np, errs
	}

	if res, err := Run(np, commands); err == nil {
		return res, np, nil
	} else {
		return res, np, []error{err}
	}
}

type testR map[string]interface{}
type testE []error

type parserTest struct {
	script    string
	condition interface{}
}

func runParserTests(tests map[string]parserTest, t *testing.T) {
	for index, test := range tests {
		res, np, errs := testRunAndReturnErrors(test.script)

		switch test.condition.(type) {
		case func(testR, testE) bool:

			if !test.condition.(func(testR, testE) bool)(res, errs) {
				for _, err := range errs {
					t.Errorf("Test %s -> error %s", index, err)
				}

				t.Errorf("Test %s fails condition: Got %#v", index, res)
			}

		case func(*dummyNotificationProvider) bool:

			if !test.condition.(func(*dummyNotificationProvider) bool)(np) {
				for _, err := range errs {
					t.Errorf("Test %s -> error %s", index, err)
				}

				t.Errorf("Test %s fails condition: Got %#v", index, res)
			}

		default:
			if len(errs) != 0 {
				for _, err := range errs {
					t.Errorf("Test %s -> error %s", index, err)
				}
			}

			if res["a"] != test.condition {
				t.Errorf("Unexpected result when running test %s: Wanted %T(%#v), got %T(%#v) instead", index, test.condition, test.condition, res["a"], res["a"])
			}
		}
	}
}

func TestBasicExpressions(t *testing.T) {
	tests := map[string]parserTest{
		"Numeric expression":       {"a:123", 123.0},
		"Addition":                 {"a:123+10", 133.0},
		"Multiplication":           {"a:10*10", 100.0},
		"Division":                 {"a:100/5", 20.0},
		"Subtraction":              {"a:132-10", 122.0},
		"Arithmetic precedence":    {"a:123+10*10", 223.0},
		"Parentheses":              {"a:(123+10)*10", 1330.0},
		"Unary Minus":              {"a:123+-10", 113.0},
		"Unary Minus + precedence": {"a:-(123+10)*10", -1330.0},
		"Variable assignment":      {"$a:10; a:$a+10", 20.0},
		"Arithmetic deviance":      {`a:"test"+10`, func(res testR, errs testE) bool { return len(errs) == 1 }},
		"Variable transassignment": {`$a: series("cpu_load"); a: $a.last()`, func(res testR, errs testE) bool { _, ok := res["a"].(float64); return ok }},
	}

	runParserTests(tests, t)
}

func TestGlobalMethods(t *testing.T) {
	checkFloat := func(res testR, errs testE) bool {
		_, ok := res["a"].(float64)

		return ok
	}

	checkNotification := func(count int) func(*dummyNotificationProvider) bool {
		return func(np *dummyNotificationProvider) bool {
			return len(np.notifications) == 1
		}
	}

	tests := map[string]parserTest{
		"Global.now()":            {"a:now()", checkFloat},
		"Global.now() assignment": {"$a:now(); a:$a", checkFloat},
		"Global.notify()":         {`notify(channel:"123",title:"test",duration:"10s",message:"Hello")`, checkNotification(1)},
	}

	runParserTests(tests, t)
}

func TestSeries(t *testing.T) {
	checkFloat := func(res testR, errs testE) bool {
		_, ok := res["a"].(float64)

		return ok
	}

	checkArray := func(count int) func(res testR, errs testE) bool {
		return func(res testR, errs testE) bool {
			r, ok := res["a"].([]interface{})

			if ok {
				if len(r) != count {
					t.Errorf("Returned data should contain %d elements, but only %d found", count, len(r))
				}

				for _, rr := range r {
					if index, ok := rr.(float64); !ok {
						t.Errorf("Value %d in returned data is not a float.", index)
						return false
					}
				}
			}

			return ok
		}
	}

	tests := map[string]parserTest{
		"Series.last()":      {`a:series("cpu_load").last()+10`, checkFloat},
		"Series.aggregate()": {`a:series("cpu_load").aggregate(func:"avg",interval:"10s",count:50)`, checkArray(50)},
		"Series.avg()":       {`a:series("cpu_load").avg("10m")+10`, checkFloat},
		"Series.sum()":       {`a:series("cpu_load").avg("10m")+10`, checkFloat},
		"Series.count()":     {`a:series("cpu_load").count("10m")+10`, checkFloat},
		"Series.min()":       {`a:series("cpu_load").min("10m")+10`, checkFloat},
		"Series.max()":       {`a:series("cpu_load").max("10m")+10`, checkFloat},
	}

	runParserTests(tests, t)
}

func TestCounter(t *testing.T) {
	tests := map[string]parserTest{
		"Counter.set":       {`$a:counter("test123"); $a.set(100); a:counter("test123")`, 100.0},
		"Counter.increment": {`$a:counter("test123"); $a.set(100); $a.increment(100); a:counter("test123")`, 200.0},
		"Counter.reset":     {`$a:counter("test123"); $a.reset(); a:counter("test123")`, 0.0},
	}

	runParserTests(tests, t)
}

func TestBooleanAndLogicOperations(t *testing.T) {
	checkBool := func(expect bool) func(res testR, errs testE) bool {
		return func(res testR, errs testE) bool {
			r, ok := res["a"].(bool)

			if ok {
				return r == expect
			}

			return ok
		}
	}

	tests := map[string]parserTest{
		"Boolean false assignment": {"a:false", checkBool(false)},
		"Boolean true assignment":  {"a:true", checkBool(true)},
		"Boolean or 1":             {"a:true||true", checkBool(true)},
		"Boolean or 2":             {"a:true||false", checkBool(true)},
		"Boolean or 3":             {"a:false||true", checkBool(true)},
		"Boolean or 4":             {"a:false||false", checkBool(false)},
		"Boolean and 1":            {"a:true&&true", checkBool(true)},
		"Boolean and 2":            {"a:true&&false", checkBool(false)},
		"Boolean and 3":            {"a:false&&true", checkBool(false)},
		"Boolean and 4":            {"a:false&&false", checkBool(false)},
		"Equality 1":               {"a:true==true", checkBool(true)},
		"Equality 2":               {"a:true==false", checkBool(false)},
		"Equality 3":               {"a:false==true", checkBool(false)},
		"Equality 4":               {"a:false==false", checkBool(true)},
		"Equality 5":               {"a:10==10", checkBool(true)},
		"Equality 5.1":             {"a:10==11", checkBool(false)},
		"Equality 6":               {`a:10=="10"`, checkBool(true)},
		"Equality 6.1":             {`a:10=="11"`, checkBool(false)},
		"Equality 7":               {`a:"test"=="test"`, checkBool(true)},
		"Equality 7.1":             {`a:"test"=="test1"`, checkBool(false)},
		"Inequality 1":             {"a:true!=true", checkBool(false)},
		"Inequality 2":             {"a:true!=false", checkBool(true)},
		"Inequality 3":             {"a:false!=true", checkBool(true)},
		"Inequality 4":             {"a:false!=false", checkBool(false)},
		"Inequality 5":             {"a:10!=10", checkBool(false)},
		"Inequality 5.1":           {"a:10!=11", checkBool(true)},
		"Inequality 6":             {`a:10!="10"`, checkBool(false)},
		"Inequality 6.1":           {`a:10!="11"`, checkBool(true)},
		"Inequality 7":             {`a:"test"!="test"`, checkBool(false)},
		"Inequality 7.1":           {`a:"test"!="test1"`, checkBool(true)},
	}

	runParserTests(tests, t)
}

func TestIfThenElse(t *testing.T) {
	tests := map[string]parserTest{
		"If then":      {"if true==true{a:10}", 10.0},
		"If then else": {"if false==true{a:10}else{a:20}", 20.0},
	}

	runParserTests(tests, t)
}