// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type conformanceAirtableRetryContext struct {
	*stepDexContext
	attempt   int32
	heartbeat []byte
}

func (context *conformanceAirtableRetryContext) Attempt() int32 { return context.attempt }
func (context *conformanceAirtableRetryContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *conformanceAirtableRetryContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := airtable.New(airtable.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[airtable.Credentials]{
		airtableConnection: {PersonalAccessToken: sdkgo.NewSecretString(testAccessToken)},
	}, airtable.WithHTTPClient(transport))
	require.NoError(t, err)
	context := &conformanceAirtableRetryContext{stepDexContext: newStepDexContext("conformance"), attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(checkpoint []byte) { context.heartbeat = checkpoint })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listRecords":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListRecords(), airtableConnection),
		"getRecord":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetRecord(), airtableConnection),
		"upsertRecords": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertRecords(), airtableConnection, scenario.CallID),
		"updateRecords": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateRecords(), airtableConnection, scenario.CallID),
		"createRecord":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateRecord(), airtableConnection, scenario.CallID),
		"deleteRecord":  newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteRecord(), airtableConnection, scenario.CallID),
	}}
}
