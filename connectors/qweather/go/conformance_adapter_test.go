// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qweather

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{APIHost: sdkgo.NewSecretString("abc123.xy.qweatherapi.com"), APIKey: sdkgo.NewSecretString("private-test-key")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getCurrentWeather": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCurrentWeather(), testConnection),
	}}
}
