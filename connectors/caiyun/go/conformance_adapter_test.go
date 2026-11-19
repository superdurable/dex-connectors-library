// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package caiyun

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

var conformanceRequestTime = time.Date(2026, 10, 22, 7, 26, 40, 0, time.UTC)
var conformanceNonceBytes = []byte{0x01, 0x95, 0xc6, 0x8a, 0x42, 0xe7, 0x72, 0x43, 0xbf, 0xf2, 0xac, 0x97, 0xa7, 0x8b, 0x83, 0x7d}

type conformanceSigningOption struct{}

func (conformanceSigningOption) applyClientOption(client *Client) {
	client.clock = func() time.Time { return conformanceRequestTime }
	client.nonceSource = bytes.NewReader(conformanceNonceBytes)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	if scenario.Name == "getCurrentWeather-missing-credentials" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "getCurrentWeather-oversized" {
		config.MaxResponseBytes = 8
	}
	client, err := New(config, credentials, WithHTTPClient(transport), conformanceSigningOption{})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getCurrentWeather": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCurrentWeather(), testConnection),
	}}
}
