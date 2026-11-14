// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oceanbase

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessKeyID = "test-access-key-id"
const testAccessKeySecret = "test-access-key-secret"
const testProjectID = "project-123"

var testConnection = sdkgo.ConnectionRef{Provider: "oceanbase", Name: "oceanbase"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "oceanbase-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-instances" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func reply(status int, body string, challenge string) *http.Response {
	response := &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
	if challenge != "" {
		response.Header.Set("WWW-Authenticate", challenge)
	}
	return response
}
func newTestClient(t *testing.T, site Site, transport http.RoundTripper) *Client {
	t.Helper()
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), AccessKeySecret: sdkgo.NewSecretString(testAccessKeySecret), ProjectID: sdkgo.NewSecretString(testProjectID)}
	client, err := New(Config{Site: site}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}

const instancesJSON = `{"success":true,"data":{"dataList":[{"instanceId":"obcluster-1","instanceName":"demo","state":"ONLINE","region":"ap-guangzhou"}]},"totalCount":4}`

func TestDigestChallengeAndInstancePage(t *testing.T) {
	for _, algorithm := range []string{"MD5", "SHA-256"} {
		t.Run(algorithm, func(t *testing.T) {
			var calls atomic.Int32
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				callNumber := calls.Add(1)
				require.Equal(t, "/api/v2/instances", request.URL.Path)
				require.Equal(t, "2", request.URL.Query().Get("pageNumber"))
				require.Equal(t, "3", request.URL.Query().Get("pageSize"))
				require.NotEmpty(t, request.URL.Query().Get("requestId"))
				require.Equal(t, testProjectID, request.Header.Get("X-Ob-Project-Id"))
				require.Equal(t, "https", request.URL.Scheme)
				require.Equal(t, "api-cloud-cn.oceanbase.com", request.URL.Host)
				if callNumber == 1 {
					require.Empty(t, request.Header.Get("Authorization"))
					return reply(401, "", `Digest realm="oceanbase", nonce="server-nonce", qop="auth", opaque="opaque-value", algorithm=`+algorithm), nil
				}
				require.EqualValues(t, 2, callNumber)
				authorization := request.Header.Get("Authorization")
				require.NotContains(t, authorization, testAccessKeySecret)
				values := parseAuthorizationParameters(authorization)
				require.Equal(t, testAccessKeyID, values["username"])
				require.Equal(t, "oceanbase", values["realm"])
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
				ha1 := hash(testAccessKeyID + ":oceanbase:" + testAccessKeySecret)
				ha2 := hash("GET:" + request.URL.RequestURI())
				require.Equal(t, hash(ha1+":server-nonce:00000001:"+values["cnonce"]+":auth:"+ha2), values["response"])
				return reply(200, instancesJSON, ""), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, SiteDomestic, transport).ListInstances(), testConnection, ListInstancesInput{PageNumber: 2, PageSize: 3})
			require.NoError(t, err)
			require.Equal(t, ListInstancesBranchListed, result.Branch)
			require.EqualValues(t, 4, result.Value.Total)
			require.EqualValues(t, 2, result.Value.PageNumber)
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
	}{{"malformed challenge", 401, `Digest realm="x", qop="auth"`, ListInstancesBranchProviderRejected, false}, {"stale challenge", 401, `Digest realm="x", nonce="n", qop="auth", stale=true`, ListInstancesBranchProviderRejected, false}, {"second 401", 401, `Digest realm="x", nonce="n", qop="auth"`, ListInstancesBranchProviderRejected, false}, {"rate limit", 429, "", "", true}, {"server failure", 503, "", "", true}, {"redirect", 302, "", ListInstancesBranchProviderRejected, false}}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			var calls atomic.Int32
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				response := reply(scenario.status, "private provider data", scenario.challenge)
				response.Header.Set("Retry-After", "2")
				if scenario.status == 302 {
					response.Header.Set("Location", "https://example.com/collect")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, SiteInternational, transport).ListInstances(), testConnection, ListInstancesInput{})
			if scenario.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAccessKeySecret)
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "private provider data")
			if scenario.name == "second 401" {
				require.EqualValues(t, 2, calls.Load())
			} else {
				require.EqualValues(t, 1, calls.Load())
			}
		})
	}
}
func TestInstanceResponseValidation(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{"success":false,"errorMessage":"secret"}`, `{"success":true,"data":{"dataList":[]},"totalCount":-1}`, `{"success":true,"data":{"dataList":[{}]},"totalCount":1}`, strings.Repeat("x", 2<<20)} {
		t.Run(body[:min(len(body), 20)], func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, body, ""), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, SiteDomestic, transport).ListInstances(), testConnection, ListInstancesInput{})
			require.NoError(t, err)
			if strings.Contains(body, `"success":false`) {
				require.Equal(t, ListInstancesBranchProviderRejected, result.Branch)
			} else {
				require.Equal(t, ListInstancesBranchInvalidResponse, result.Branch)
			}
		})
	}
}
func TestInvalidInputAndCredentialsSendNoRequest(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid input or credentials must not send request")
		return nil, errors.New("unexpected")
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, SiteDomestic, transport).ListInstances(), testConnection, ListInstancesInput{PageNumber: -1})
	require.NoError(t, err)
	require.Equal(t, ListInstancesBranchDefect, result.Branch)
	badClient, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKeyID: sdkgo.NewSecretString("bad key"), AccessKeySecret: sdkgo.NewSecretString(testAccessKeySecret), ProjectID: sdkgo.NewSecretString(testProjectID)}}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, badClient.ListInstances(), testConnection, ListInstancesInput{})
	require.NoError(t, err)
	require.Equal(t, ListInstancesBranchDefect, result.Branch)
}

func TestTransportFailureDoesNotExposeAccessKey(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("failed with " + testAccessKeySecret + " and " + testProjectID)
	})
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, SiteDomestic, transport).ListInstances(), testConnection, ListInstancesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), testAccessKeySecret)
	require.NotContains(t, retry.Error(), testProjectID)
}
