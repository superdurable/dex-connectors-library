// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postmark

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type sendConformanceContext struct {
	providerTestContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *sendConformanceContext) Attempt() int32 { return context.attempt }
func (context *sendConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}
func (context *sendConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "postmark", Name: "postmark"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: {APIKey: sdkgo.NewSecretString("test-secret-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	sendContext := &sendConformanceContext{attempt: 1}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { sendContext.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listOutboundMessages": newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListOutboundMessages(), connection),
		"getMessageDetails":    newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetMessageDetails(), connection),
		"sendEmail":            newConformanceMutationInvocation(t, scenario.Input, sendContext, client.SendEmail(), connection, scenario.CallID),
		"listTemplates":        newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListTemplates(), connection),
		"getTemplate":          newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetTemplate(), connection),
		"createTemplate":       newConformanceMutationInvocation(t, scenario.Input, sendContext, client.CreateTemplate(), connection, scenario.CallID),
		"updateTemplate":       newConformanceMutationInvocation(t, scenario.Input, sendContext, client.UpdateTemplate(), connection, scenario.CallID),
		"deleteTemplate":       newConformanceMutationInvocation(t, scenario.Input, sendContext, client.DeleteTemplate(), connection, scenario.CallID),
		"listBounces":          newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListBounces(), connection),
		"getBounce":            newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetBounce(), connection),
		"activateBounce":       newConformanceMutationInvocation(t, scenario.Input, sendContext, client.ActivateBounce(), connection, scenario.CallID),
	}}
}
