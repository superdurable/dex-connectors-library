// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package esign

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type conformanceClockOption struct{}

func (conformanceClockOption) applyClientOption(client *Client) {
	client.clock = func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	config := Config{}
	if scenario.Name == "getSignFlowStatus-oversize" {
		config.MaxResponseBytes = 16
	}
	credentials := Credentials{AppID: "app-123", AppSecret: sdkgo.NewSecretString("test-secret")}
	if scenario.Name == "getSignFlowStatus-missing-secret" {
		credentials = Credentials{AppID: "app-123"}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), conformanceClockOption{})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getSignFlowStatus": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSignFlowStatus(), testConnection),
	}}
}
