// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/rest/v6"
const conformanceFixturePathPrefix = "/api/rest/v6"

type conformanceReminderRetryContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *conformanceReminderRetryContext) Attempt() int32 { return context.attempt }
func (context *conformanceReminderRetryContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *conformanceReminderRetryContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "adobe-acrobat-sign", Name: "adobe-acrobat-sign"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {IntegrationKey: sdkgo.NewSecretString("sample-acrobat-sign-integration-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	retryContext := &conformanceReminderRetryContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(checkpoint []byte) { retryContext.heartbeat = checkpoint })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { retryContext.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getAgreement":              newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAgreement(), reference),
		"listAgreements":            newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAgreements(), reference),
		"updateAgreementVisibility": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAgreementVisibility(), reference, scenario.CallID),
		"cancelAgreement":           newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelAgreement(), reference, scenario.CallID),
		"createAgreementReminder":   newConformanceMutationInvocation(t, scenario.Input, retryContext, client.CreateAgreementReminder(), reference, scenario.CallID),
	}}
}
