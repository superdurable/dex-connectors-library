// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bilibili

import (
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""
const conformanceNonce = "00000000000000000000000000000000"
const conformanceTimestamp = "1730185575"

type canonicalSigningTransport struct { next http.RoundTripper; t *testing.T }
func (transport canonicalSigningTransport) RoundTrip(request *http.Request) (*http.Response, error) {
    headers := map[string]string{}
    for _, key := range []string{"x-bili-accesskeyid", "x-bili-content-md5", "x-bili-signature-method", "x-bili-signature-nonce", "x-bili-signature-version", "x-bili-timestamp"} {
        headers[key] = request.Header.Get(key)
    }
    require.Equal(transport.t, signRequest("secret456", headers), request.Header.Get("Authorization"))
    request.Header.Set("X-Bili-Signature-Nonce", conformanceNonce)
    request.Header.Set("X-Bili-Timestamp", conformanceTimestamp)
    headers["x-bili-signature-nonce"] = conformanceNonce
    headers["x-bili-timestamp"] = conformanceTimestamp
    request.Header.Set("Authorization", signRequest("secret456", headers))
    return transport.next.RoundTrip(request)
}
func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    credentials := Credentials{ClientID: "client123", AppSecret: sdkgo.NewSecretString("secret456"), AccessToken: sdkgo.NewSecretString("token789")}
    if scenario.Name == "getAuthorizedUser-defect" { credentials = Credentials{} }
    client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: canonicalSigningTransport{next: transport.Transport, t: t}}))
    require.NoError(t, err)
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "getAuthorizedUser": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAuthorizedUser(), testConnection),
    }}
}
