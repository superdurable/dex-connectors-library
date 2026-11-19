// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qichacha

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppKey: sdkgo.NewSecretString("REDACTED_QCC_APP_KEY"), SecretKey: sdkgo.NewSecretString("REDACTED_QCC_SECRET")}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	client.now = conformanceFixedClock(t, scenario)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getEnterpriseBasicDetails": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetEnterpriseBasicDetails(), testConnection),
	}}
}
