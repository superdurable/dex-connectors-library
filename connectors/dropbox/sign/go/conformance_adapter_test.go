// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropboxsign

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = "/v3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "dropbox-sign", Name: "dropbox-sign"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-dropbox-sign-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("dropbox-sign-provider-test", "action")
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
		"listTemplates":                    newConformanceQueryInvocation(t, scenario.Input, context, client.ListTemplates(), reference),
		"getTemplate":                      newConformanceQueryInvocation(t, scenario.Input, context, client.GetTemplate(), reference),
		"deleteTemplate":                   newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteTemplate(), reference, scenario.CallID),
		"sendSignatureRequest":             newConformanceMutationInvocation(t, scenario.Input, context, client.SendSignatureRequest(), reference, scenario.CallID),
		"sendSignatureRequestWithTemplate": newConformanceMutationInvocation(t, scenario.Input, context, client.SendSignatureRequestWithTemplate(), reference, scenario.CallID),
		"cancelIncompleteSignatureRequest": newConformanceMutationInvocation(t, scenario.Input, context, client.CancelIncompleteSignatureRequest(), reference, scenario.CallID),
		"remindSignatureRequest":           newConformanceMutationInvocation(t, scenario.Input, context, client.RemindSignatureRequest(), reference, scenario.CallID),
		"getSignatureRequest":              newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSignatureRequest(), reference),
		"listSignatureRequests":            newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSignatureRequests(), reference),
		"updateSignerName":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateSignerName(), reference, scenario.CallID),
	}}
}
