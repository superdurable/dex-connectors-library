// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package ses

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
	"time"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "amazon-ses", Name: "amazon-ses"}
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKIASESCONNECTOR"), SecretAccessKey: sdkgo.NewSecretString("ses-test-secret"), SessionToken: sdkgo.NewSecretString("")}
	client, err := New(Config{Endpoint: endpoint, Region: "eu-west-1"}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport), signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	sendContext := &conformanceSendContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(checkpoint []byte) { sendContext.heartbeat = checkpoint })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { sendContext.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getEmailIdentity":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetEmailIdentity(), reference),
		"listEmailIdentities": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListEmailIdentities(), reference),
		"deleteEmailIdentity": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteEmailIdentity(), reference, scenario.CallID),
		"sendEmail":           newConformanceMutationInvocation(t, scenario.Input, sendContext, client.SendEmail(), reference, scenario.CallID),
	}}
}

type conformanceSendContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *conformanceSendContext) Attempt() int32 { return context.attempt }
func (context *conformanceSendContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}
func (context *conformanceSendContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
