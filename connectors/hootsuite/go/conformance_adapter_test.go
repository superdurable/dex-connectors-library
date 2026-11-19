// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "hootsuite", Name: "hootsuite"}
	credentials := Credentials{
		OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-hootsuite-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
	}
	if scenario.Name == "listSocialProfiles-defect" {
		credentials.AccessToken = sdkgo.NewSecretString("")
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	scheduleContext := singledispatchtest.NewContext("hootsuite-conformance", "schedule-post")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			scheduleContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listSocialProfiles": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSocialProfiles(), reference),
		"listScheduledPosts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListScheduledPosts(), reference),
		"schedulePost":       newConformanceMutationInvocation(t, scenario.Input, scheduleContext, client.SchedulePost(), reference, scenario.CallID),
	}}
}
