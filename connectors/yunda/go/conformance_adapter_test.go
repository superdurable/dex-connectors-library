// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yunda

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
    credentials := Credentials{
        AppKey: sdkgo.NewSecretString("999999"),
        AppSecret: sdkgo.NewSecretString("REDACTED_APP_SECRET"),
        PartnerID: sdkgo.NewSecretString("REDACTED_PARTNER_ID"),
        BaimaSecret: sdkgo.NewSecretString("REDACTED_BAIMA_SECRET"),
    }
    if scenario.Name == "getWaybillBalance-defect" { credentials = Credentials{} }
    configuration := Config{}
    if scenario.Name == "getWaybillBalance-oversized" { configuration.MaxResponseBytes = 16 }
    client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), WithClock(func() time.Time { return time.UnixMilli(1731510000000) }))
    require.NoError(t, err)
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "getWaybillBalance": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetWaybillBalance(), testConnection),
    }}
}
