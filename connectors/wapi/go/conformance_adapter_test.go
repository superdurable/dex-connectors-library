// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type conformanceClockOption struct{ now func() time.Time }

func (option conformanceClockOption) applyClientOption(client *Client) {
	client.requestClock = option.now
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppID: "123", AppSecret: sdkgo.NewSecretString("REDACTEDAPPSECRET000000000000000")}
	if scenario.Name == "listVehicleBrands-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(transport), conformanceClockOption{conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listVehicleBrands": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListVehicleBrands(), testConnection),
	}}
}
