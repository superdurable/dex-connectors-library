// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/go"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := tokenCredentials()
	if scenario.Name == "submit-key-pair-jwt" {
		key, privateKeyPEM := testPrivateKey(t)
		credentials = snowflake.Credentials{AuthMethodID: snowflake.KeyPairAuthMethodID, User: "dex_service", PrivateKey: sdkgo.NewSecretString(privateKeyPEM)}
		transport.Transport = &conformanceJWTTransport{next: transport.Transport, publicKey: &key.PublicKey, t: t}
	}
	client, err := snowflake.New(snowflake.Config{AccountIdentifier: "myorg-analytics", Warehouse: "REPORTING_WH", Role: "DEX_REPORTING", Database: "ANALYTICS", Schema: "BILLING", MaxRows: 1},
		sdkgo.StaticCredentialProvider[snowflake.Credentials]{testConnection: credentials},
		snowflake.WithLocalProviderURL(endpoint), snowflake.WithHTTPClient(transport), snowflake.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	context := testsupport.NewDexContext("snowflake-conformance", "statement")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"submitStatement":    newConformanceMutationInvocation(t, scenario.Input, context, client.SubmitStatement(), testConnection, scenario.CallID),
		"getStatementResult": newConformanceQueryInvocation(t, scenario.Input, context, client.GetStatementResult(), testConnection),
		"cancelStatement":    newConformanceMutationInvocation(t, scenario.Input, context, client.CancelStatement(), testConnection, scenario.CallID),
	}}
}

type conformanceJWTTransport struct {
	next      http.RoundTripper
	publicKey *rsa.PublicKey
	t         *testing.T
}

func (transport *conformanceJWTTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.t.Helper()
	token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	require.Len(transport.t, parts, 3)
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(transport.t, err)
	require.JSONEq(transport.t, `{"alg":"RS256","typ":"JWT"}`, string(header))
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(transport.t, err)
	var claims map[string]any
	require.NoError(transport.t, json.Unmarshal(claimsBytes, &claims))
	require.Equal(transport.t, "MYORG-ANALYTICS.DEX_SERVICE", claims["sub"])
	require.Equal(transport.t, float64(fixedNow.Unix()), claims["iat"])
	require.Equal(transport.t, float64(fixedNow.Unix()+3540), claims["exp"])
	publicDER, err := x509.MarshalPKIXPublicKey(transport.publicKey)
	require.NoError(transport.t, err)
	fingerprint := sha256.Sum256(publicDER)
	require.Equal(transport.t, "MYORG-ANALYTICS.DEX_SERVICE.SHA256:"+base64.StdEncoding.EncodeToString(fingerprint[:]), claims["iss"])
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(transport.t, err)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	require.NoError(transport.t, rsa.VerifyPKCS1v15(transport.publicKey, crypto.SHA256, digest[:], signature))
	request.Header.Set("Authorization", "Bearer conformance-key-pair-jwt")
	return transport.next.RoundTrip(request)
}
