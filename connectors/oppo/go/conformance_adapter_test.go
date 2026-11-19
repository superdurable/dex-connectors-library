// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oppo

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
	credentials := Credentials{AppKey: sdkgo.NewSecretString("test-app-key"), MasterSecret: sdkgo.NewSecretString("test-master-secret")}
	if scenario.Name == "getPushQuota-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), WithClock(func() time.Time { return time.UnixMilli(1731510000000) }))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getPushQuota": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPushQuota(), testConnection),
	}}
}
