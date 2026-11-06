// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := pipedrive.New(pipedrive.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[pipedrive.Credentials]{
		pipedriveConnection: {AuthMethodID: pipedrive.APITokenAuthMethodID, APIToken: sdkgo.NewSecretString(testAPIToken)},
	}, pipedrive.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newStepDexContext("conformance")
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		context.attempt = int32(attempt) + priorAttempt
	}, actions: map[string]conformanceOperationInvocation{
		"searchObjects": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchObjects(), pipedriveConnection),
		"getObject":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetObject(), pipedriveConnection),
		"listObjects":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListObjects(), pipedriveConnection),
		"upsertObject":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertObject(), pipedriveConnection, scenario.CallID),
		"createObject":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateObject(), pipedriveConnection, scenario.CallID),
		"updateObject":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateObject(), pipedriveConnection, scenario.CallID),
	}}
}
