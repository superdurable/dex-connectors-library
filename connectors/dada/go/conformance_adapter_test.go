// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dada

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
	config := Config{BaseURL: endpoint}
	if scenario.Name == "queryOrderStatus-oversize" {
		config.MaxResponseBytes = 5
	}
	credentials := Credentials{AppKey: "123456", AppSecret: sdkgo.NewSecretString("abcdefg123"), SourceID: "73753"}
	if scenario.Name == "queryOrderStatus-missing-credentials" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: func() time.Time { return time.Unix(1488363493, 0).UTC() }})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryOrderStatus": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryOrderStatus(), testConnection),
	}}
}
