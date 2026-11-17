// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropbox

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := testCredentials()
	if scenario.Name == "listFolder-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint, ContentBaseURL: endpoint, MaxDownloadBytes: 8, MaxUploadBytes: 8}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("dropbox-provider-test", "query")
	priorAttempt := 0
	if len(scenario.PriorHeartbeat) > 0 {
		var marker struct {
			Attempt int             `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		checkpoint := scenario.PriorHeartbeat
		if len(marker.Value) > 0 {
			checkpoint = marker.Value
		}
		require.NoError(t, context.SetPersistedCheckpoint(checkpoint))
		priorAttempt = marker.Attempt
		for range priorAttempt {
			context.AdvanceAttempt()
		}
	}
	currentAttempt := priorAttempt + 1
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		for currentAttempt < priorAttempt+attempt {
			context.AdvanceAttempt()
			currentAttempt++
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listFolder":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListFolder(), testConnection),
		"getMetadata":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetMetadata(), testConnection),
		"continueFolder": newConformanceQueryInvocation(t, scenario.Input, context, client.ContinueFolder(), testConnection),
		"downloadFile":   newConformanceQueryInvocation(t, scenario.Input, context, client.DownloadFile(), testConnection),
		"uploadFile":     newConformanceMutationInvocation(t, scenario.Input, context, client.UploadFile(), testConnection, scenario.CallID),
		"searchFiles":    newConformanceQueryInvocation(t, scenario.Input, context, client.SearchFiles(), testConnection),
		"ensureFolder":   newConformanceMutationInvocation(t, scenario.Input, context, client.EnsureFolder(), testConnection, scenario.CallID),
		"deletePath":     newConformanceMutationInvocation(t, scenario.Input, context, client.DeletePath(), testConnection, scenario.CallID),
		"copyPath":       newConformanceMutationInvocation(t, scenario.Input, context, client.CopyPath(), testConnection, scenario.CallID),
		"movePath":       newConformanceMutationInvocation(t, scenario.Input, context, client.MovePath(), testConnection, scenario.CallID),
	}}
}
