// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rabbitmq

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "rabbitmq", Name: "rabbitmq"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		reference: {Username: "observer", Password: sdkgo.NewSecretString("sample-password")},
	}
	if strings.HasSuffix(scenario.Name, "-credentials-unavailable") {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{ManagementURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := singledispatchtest.NewContext("rabbitmq-conformance", "publish-message")
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) {
		var checkpoint any
		require.NoError(t, json.Unmarshal(value, &checkpoint))
		require.NoError(t, mutationContext.SetPersistedCheckpoint(checkpoint))
	})
	for range priorAttempt {
		mutationContext.AdvanceAttempt()
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			mutationContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getQueueMetrics": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetQueueMetrics(), reference),
		"publishMessage":  newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.PublishMessage(), reference, scenario.CallID),
	}}
}
