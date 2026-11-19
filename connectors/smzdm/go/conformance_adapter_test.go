// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smzdm

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppKey: sdkgo.NewSecretString("123456789"), AppSecret: sdkgo.NewSecretString("88888888")}
	if scenario.Name == "searchProducts-defect" {
		credentials = Credentials{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "searchProducts-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: func() time.Time { return time.Unix(1731510000, 0) }})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchProducts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchProducts(), testConnection),
	}}
}
