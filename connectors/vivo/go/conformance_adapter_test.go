// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vivo

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
	credentials := Credentials{Account: sdkgo.NewSecretString("test-account"), Secret: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), WithClock(func() time.Time { return time.UnixMilli(1731510000000) }))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getOnlineVersion": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOnlineVersion(), testConnection),
	}}
}
