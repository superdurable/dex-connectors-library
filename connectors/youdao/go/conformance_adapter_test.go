// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youdao

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	configuration := Config{BaseURL: endpoint}
	if scenario.Name == "translateText-oversized" {
		configuration.MaxResponseBytes = 5
	}
	credentials := Credentials{AppKey: "test-app", AppSecret: sdkgo.NewSecretString("REDACTED_APP_SECRET")}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)},
		randomSourceOption{source: bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"translateText": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.TranslateText(), testConnection),
	}}
}
