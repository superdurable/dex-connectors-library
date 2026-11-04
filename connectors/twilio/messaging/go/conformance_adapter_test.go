// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/2010-04-01"
const conformanceFixturePathPrefix = "/2010-04-01"

type twilioConformanceContext struct {
	*testDexContext
	mutex     sync.Mutex
	attempt   int32
	heartbeat json.RawMessage
}

func (context *twilioConformanceContext) Attempt() int32 { return context.attempt }
func (context *twilioConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.mutex.Lock()
	defer context.mutex.Unlock()
	context.heartbeat = encoded
	return nil
}
func (context *twilioConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	context.mutex.Lock()
	defer context.mutex.Unlock()
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	accountSID := "AC" + strings.Repeat("0", 32)
	client, err := messaging.New(
		messaging.Config{AccountSID: accountSID, DefaultSender: testSender, Endpoint: endpoint},
		sdkgo.StaticCredentialProvider[messaging.Credentials]{twilioConnection: {
			AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(testAuthToken),
		}},
		messaging.WithHTTPClient(transport),
	)
	require.NoError(t, err)
	context := &twilioConformanceContext{testDexContext: newTestDexContext("conformance"), attempt: 1, heartbeat: append(json.RawMessage(nil), scenario.PriorHeartbeat...)}
	startAttempt := int32(0)
	if len(scenario.PriorHeartbeat) > 0 {
		var prior struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
		startAttempt = prior.Attempt
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt) + startAttempt }, actions: map[string]conformanceOperationInvocation{
		"sendMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.SendMessage(), twilioConnection, scenario.CallID),
		"getMessage":  newConformanceQueryInvocation(t, scenario.Input, context, client.GetMessage(), twilioConnection),
	}}
}
