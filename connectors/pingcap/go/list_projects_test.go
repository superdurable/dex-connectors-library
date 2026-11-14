// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pingcap

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testPublicKey = "test-public-key"
const testPrivateKey = "test-private-key"

var testConnection = sdkgo.ConnectionRef{Provider: "pingcap", Name: "pingcap"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "pingcap-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-projects" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PublicKey: sdkgo.NewSecretString(testPublicKey), PrivateKey: sdkgo.NewSecretString(testPrivateKey)}})
	require.NoError(t, err)
	return client
}
func TestDigestChallengeAndProjectPage(t *testing.T) {
	for _, algorithm := range []string{"MD5", "SHA-256"} {
		t.Run(algorithm, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				callNumber := calls.Add(1)
				require.Equal(t, "/api/v1beta/projects", request.URL.Path)
				require.Equal(t, "2", request.URL.Query().Get("page"))
				require.Equal(t, "3", request.URL.Query().Get("page_size"))
				if callNumber == 1 {
					require.Empty(t, request.Header.Get("Authorization"))
					writer.Header().Set("WWW-Authenticate", `Digest realm="tidbcloud", nonce="server-nonce", qop="auth", opaque="opaque-value", algorithm=`+algorithm)
					writer.WriteHeader(401)
					return
				}
				require.EqualValues(t, 2, callNumber)
				authorization := request.Header.Get("Authorization")
				require.NotContains(t, authorization, testPrivateKey)
				values := parseAuthorizationParameters(authorization)
				require.Equal(t, testPublicKey, values["username"])
				require.Equal(t, "tidbcloud", values["realm"])
				require.Equal(t, "server-nonce", values["nonce"])
				require.Equal(t, "opaque-value", values["opaque"])
				require.Equal(t, "auth", values["qop"])
				require.Equal(t, "00000001", values["nc"])
				require.Equal(t, algorithm, values["algorithm"])
				require.Equal(t, request.URL.RequestURI(), values["uri"])
				hash := func(value string) string {
					if algorithm == "SHA-256" {
						sum := sha256.Sum256([]byte(value))
						return hex.EncodeToString(sum[:])
					}
					sum := md5.Sum([]byte(value))
					return hex.EncodeToString(sum[:])
				}
				ha1 := hash(testPublicKey + ":tidbcloud:" + testPrivateKey)
				ha2 := hash("GET:" + request.URL.RequestURI())
				expected := hash(ha1 + ":server-nonce:00000001:" + values["cnonce"] + ":auth:" + ha2)
				require.Equal(t, expected, values["response"])
				_, err := writer.Write([]byte(`{"items":[{"id":"101","org_id":"201","name":"demo","cluster_count":3,"user_count":1,"create_timestamp":"1652407748","type":"dedicated"}],"total":4}`))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListProjects(), testConnection, ListProjectsInput{Page: 2, PageSize: 3})
			require.NoError(t, err)
			require.Equal(t, ListProjectsBranchListed, result.Branch)
			require.EqualValues(t, 4, result.Value.Total)
			require.EqualValues(t, 2, result.Value.Page)
			require.Equal(t, "demo", result.Value.Items[0].Name)
			require.EqualValues(t, 2, calls.Load())
		})
	}
}
func parseAuthorizationParameters(header string) map[string]string {
	values := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(header, "Digest "), ", ") {
		pair := strings.SplitN(part, "=", 2)
		if len(pair) == 2 {
			values[pair[0]] = strings.Trim(pair[1], `"`)
		}
	}
	return values
}
func TestDigestChallengeValidation(t *testing.T) {
	for _, header := range []string{"", `Basic realm="x"`, `Digest realm="x", qop="auth"`, `Digest realm="x", nonce="n", qop="auth-int"`, `Digest realm="x", nonce="n", qop="auth", stale=true`, `Digest realm="x", nonce="n", qop="auth", algorithm=SHA-1`, `Digest realm="x", nonce="n", qop="auth", nonce="again"`, `Digest realm="x", nonce="n", qop="auth",`} {
		_, err := parseDigestChallenge(header)
		require.Error(t, err, header)
	}
}
func TestDigestTerminalAndRetryOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		challenge string
		branch    sdkgo.BranchID
		retry     bool
	}{{"malformed challenge", 401, `Digest realm="x", qop="auth"`, ListProjectsBranchProviderRejected, false}, {"stale challenge", 401, `Digest realm="x", nonce="n", qop="auth", stale=true`, ListProjectsBranchProviderRejected, false}, {"second 401", 401, `Digest realm="x", nonce="n", qop="auth"`, ListProjectsBranchProviderRejected, false}, {"rate limit", 429, "", "", true}, {"server failure", 503, "", "", true}, {"redirect", 302, "", ListProjectsBranchProviderRejected, false}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				writer.Header().Set("Retry-After", "2")
				if test.challenge != "" {
					writer.Header().Set("WWW-Authenticate", test.challenge)
				}
				if test.status == 302 {
					writer.Header().Set("Location", "https://example.com/collect")
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte("private provider data"))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListProjects(), testConnection, ListProjectsInput{})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testPrivateKey)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "private provider data")
			if test.name == "second 401" {
				require.EqualValues(t, 2, calls.Load())
			} else {
				require.EqualValues(t, 1, calls.Load())
			}
		})
	}
}
func TestProjectResponseValidation(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{"items":[],"total":-1}`, `{"items":[{}],"total":1}`, strings.Repeat("x", 300000)} {
		t.Run(body[:min(len(body), 20)], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte(body)) }))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListProjects(), testConnection, ListProjectsInput{})
			require.NoError(t, err)
			require.Equal(t, ListProjectsBranchInvalidResponse, result.Branch)
		})
	}
}
func TestInvalidInputAndCredentialsSendNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("invalid input or credentials must not send request")
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListProjects(), testConnection, ListProjectsInput{Page: -1})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchDefect, result.Branch)
	badClient, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PublicKey: sdkgo.NewSecretString("bad key"), PrivateKey: sdkgo.NewSecretString(testPrivateKey)}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, badClient.ListProjects(), testConnection, ListProjectsInput{})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchDefect, result.Branch)
}
