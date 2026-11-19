// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braze

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type brazeConformanceContext struct {
	queryContext
	attempt  int32
	prior    json.RawMessage
	recorded json.RawMessage
}

func (context *brazeConformanceContext) Attempt() int32 { return context.attempt }
func (context *brazeConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.prior) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.prior, value)
}
func (context *brazeConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recorded = encoded
	return nil
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "braze", Name: "braze"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {RESTAPIKey: sdkgo.NewSecretString("sample-rest-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &brazeConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.attempt = conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.prior = value }) + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.prior = append(json.RawMessage(nil), context.recorded...)
			context.recorded = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getUserProfile":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetUserProfile(), reference),
		"updateUserProfile":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateUserProfile(), reference, scenario.CallID),
		"deleteUserProfile":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteUserProfile(), reference, scenario.CallID),
		"listCampaigns":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), reference),
		"listCanvases":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCanvases(), reference),
		"listSegments":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSegments(), reference),
		"triggerCampaignSend": newConformanceMutationInvocation(t, scenario.Input, context, client.TriggerCampaignSend(), reference, scenario.CallID),
		"triggerCanvasSend":   newConformanceMutationInvocation(t, scenario.Input, context, client.TriggerCanvasSend(), reference, scenario.CallID),
	}}
}
