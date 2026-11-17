// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package aqara

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

var conformanceRequestTime = time.Date(2023, 10, 26, 10, 22, 32, 0, time.UTC)
var conformanceNonceBytes = []byte{0x31, 0x56, 0x85, 0x32, 0x99, 0xf3, 0x13, 0xe2, 0x3d, 0xc1, 0x2e, 0x17, 0x03, 0xdc, 0x12, 0xe1}

type conformanceSigningOption struct{}

func (conformanceSigningOption) applyClientOption(client *Client) {
	client.clock = func() time.Time { return conformanceRequestTime }
	client.nonceSource = bytes.NewReader(conformanceNonceBytes)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	config := Config{BaseURL: endpoint}
	if scenario.Name == "listDevices-oversized" {
		config.MaxResponseBytes = 4
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {
			AppID: "app-id", KeyID: "key-id", AppKey: sdkgo.NewSecretString("app-key"),
			AccessToken: sdkgo.NewSecretString("access-token"),
		},
	}, WithHTTPClient(transport), conformanceSigningOption{})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listDevices": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDevices(), testConnection),
	}}
}
