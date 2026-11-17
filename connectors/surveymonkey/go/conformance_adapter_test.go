// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveymonkey

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = "/v3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "surveymonkey", Name: "surveymonkey"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {AccessToken: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	dispatchContext := singledispatchtest.NewContext("surveymonkey-conformance", "create-survey")
	collectorContext := singledispatchtest.NewContext("surveymonkey-conformance", "create-collector")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			dispatchContext.AdvanceAttempt()
			collectorContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listSurveys":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSurveys(), reference),
		"getResponse":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetResponse(), reference),
		"getSurvey":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSurvey(), reference),
		"createSurvey":    newConformanceMutationInvocation(t, scenario.Input, dispatchContext, client.CreateSurvey(), reference, scenario.CallID),
		"updateSurvey":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateSurvey(), reference, scenario.CallID),
		"deleteSurvey":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteSurvey(), reference, scenario.CallID),
		"listCollectors":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCollectors(), reference),
		"getCollector":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCollector(), reference),
		"createCollector": newConformanceMutationInvocation(t, scenario.Input, collectorContext, client.CreateCollector(), reference, scenario.CallID),
		"updateCollector": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCollector(), reference, scenario.CallID),
		"deleteCollector": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCollector(), reference, scenario.CallID),
	}}
}
