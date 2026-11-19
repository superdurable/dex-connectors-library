// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailgun

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	attemptContext := &conformanceAttemptContext{attempt: 1}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { attemptContext.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getEvents":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetEvents(), testConnection),
		"listDomains": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDomains(), testConnection),
		"sendMessage": newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.SendMessage(), testConnection, scenario.CallID),
	}}
}

type conformanceAttemptContext struct {
	queryContext
	attempt       int32
	lastHeartbeat json.RawMessage
}

func (context *conformanceAttemptContext) Attempt() int32 { return context.attempt }

func (context *conformanceAttemptContext) RecordHeartbeat(value any) error {
	if value == nil {
		context.lastHeartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.lastHeartbeat = encoded
	return nil
}

func (context *conformanceAttemptContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.lastHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.lastHeartbeat, target)
}
