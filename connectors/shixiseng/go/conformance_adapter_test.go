// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shixiseng

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppID: sdkgo.NewSecretString("merchant-id"), AppSecret: sdkgo.NewSecretString("merchant-secret")}
	if scenario.Name == "listIndustries-defect" {
		credentials = Credentials{}
	}
	config := Config{}
	if scenario.Name == "listIndustries-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), WithClock(func() time.Time { return time.Unix(1731510000, 0) }))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listIndustries": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListIndustries(), testConnection),
	}}
}
