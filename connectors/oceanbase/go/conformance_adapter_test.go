// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oceanbase

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	configuration := Config{Site: SiteDomestic}
	if strings.Contains(scenario.Name, "international") {
		configuration.Site = SiteInternational
	}
	credentials := Credentials{
		AccessKeyID:     sdkgo.NewSecretString("REDACTED_OCEANBASE_ACCESS_KEY"),
		AccessKeySecret: sdkgo.NewSecretString("REDACTED_OCEANBASE_SECRET"),
		ProjectID:       sdkgo.NewSecretString("REDACTED_PROJECT_ID"),
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	client.now = conformanceFixedClock(t, scenario)
	client.randomReader = newConformanceSyntheticRandomReader(scenario.RandomSeed)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listInstances": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListInstances(), testConnection),
	}}
}
