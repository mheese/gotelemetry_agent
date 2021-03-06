package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/telemetryapp/gotelemetry"
	"github.com/telemetryapp/gotelemetry_agent/agent/config"
)

// ProcessPipeRequest processes data packets that are piped to the Agent
func ProcessPipeRequest(configFile *config.File, errorChannel chan error, completionChannel chan bool, data []byte) {
	errorChannel <- gotelemetry.NewLogError("Piped mode is on.")
	errorChannel <- gotelemetry.NewDebugError("Input data is %s", strings.Replace(string(data), "\n", "\\n", -1))

	submissionType := gotelemetry.BatchTypePATCH

	if config.CLIConfig.UseJSONPatch {
		errorChannel <- gotelemetry.NewDebugError("Will perform a JSON-Patch operation")
		submissionType = gotelemetry.BatchTypeJSONPATCH
	} else if config.CLIConfig.UsePOST {
		errorChannel <- gotelemetry.NewDebugError("Will perform a POST operation")
		submissionType = gotelemetry.BatchTypePOST
	} else {
		errorChannel <- gotelemetry.NewDebugError("Will perform a Rails-style HTTP PATCH operation")
	}

	apiToken := configFile.APIToken()

	if len(apiToken) == 0 {
		errorChannel <- fmt.Errorf("No API Token found in the configuration file or in the TELEMETRY_API_TOKEN environment variable.")
		completionChannel <- true
		return
	}

	credentials, err := gotelemetry.NewCredentials(apiToken, configFile.APIURL())

	if err != nil {
		errorChannel <- err
		completionChannel <- true

		return
	}

	credentials.SetDebugChannel(errorChannel)

	updates := map[string]interface{}{}

	err = json.Unmarshal(data, &updates)

	if err != nil {
		errorChannel <- err
		completionChannel <- true

		return
	}

	b := gotelemetry.Batch{}

	for tag, update := range updates {
		b.SetData(tag, update)
	}

	err = b.Publish(credentials, configFile.ChannelTag(), submissionType)

	if err != nil {
		errorChannel <- err
	}

	errorChannel <- gotelemetry.NewLogError("Processing complete. Exiting.")

	completionChannel <- true
}
