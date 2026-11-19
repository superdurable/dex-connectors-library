// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := salesforce.New(salesforce.Config{}, sdkgo.StaticCredentialProvider[salesforce.Credentials]{
		salesforceConnection: {
			AuthMethodID: salesforce.ProductionOAuthAuthMethodID,
			AccessToken:  sdkgo.NewSecretString(salesforceTestToken),
			InstanceURL:  endpoint,
		},
	}, salesforce.WithHTTPClient(transport))
	require.NoError(t, err)
	context := &salesforceConformanceContext{dexContext: newDexContext("conformance"), attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"queryRecords":             newConformanceQueryInvocation(t, scenario.Input, context, client.QueryRecords(), salesforceConnection),
		"getRecord":                newConformanceQueryInvocation(t, scenario.Input, context, client.GetRecord(), salesforceConnection),
		"upsertRecordByExternalId": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertRecordByExternalID(), salesforceConnection, scenario.CallID),
		"updateRecord":             newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateRecord(), salesforceConnection, scenario.CallID),
		"deleteRecord":             newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteRecord(), salesforceConnection, scenario.CallID),
		"createRecord":             newConformanceMutationInvocation(t, scenario.Input, context, client.CreateRecord(), salesforceConnection, scenario.CallID),
	}}
}

type salesforceConformanceContext struct {
	*dexContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *salesforceConformanceContext) Attempt() int32 { return context.attempt }

func (context *salesforceConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}

func (context *salesforceConformanceContext) GetLastHeartbeatValue(destination any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, destination)
}
