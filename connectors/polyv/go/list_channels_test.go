// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package polyv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAppSecret = "test-polyv-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "polyv", Name: "polyv"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "polyv-provider-test" }
func (queryContext) StepExecutionID() string     { return "channel-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ destination *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.URL.Scheme = transport.destination.Scheme
	copy.URL.Host = transport.destination.Host
	return http.DefaultTransport.RoundTrip(copy)
}
func newTestClient(t *testing.T, serverURL string, maxResponseBytes int64) *Client {
	t.Helper()
	destination, err := url.Parse(serverURL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: "testappid", AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	client, err := New(Config{MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{destination: destination}}))
	require.NoError(t, err)
	return client
}
func TestPolyvOfficialSignatureVector(t *testing.T) {
	values := url.Values{"appId": {"g4rqgmmjuo"}, "channelIds": {"2477096,2272655"}, "endDay": {"2022-06-18"}, "startDay": {"2022-05-20"}, "timestamp": {"1660270926732"}}
	require.Equal(t, "0D2BDA2FD04D93A2B8832B91FD973C4D", signPolyvParameters(values, "fsq2k5weced1h8vui657xtdva66whf0g"))
}
func TestListChannelsSignedRequestAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/live/v3/user/channels", request.URL.Path)
		values := request.URL.Query()
		require.Equal(t, "testappid", values.Get("appId"))
		require.Len(t, values.Get("timestamp"), 13)
		require.Equal(t, signPolyvParameters(values, testAppSecret), values.Get("sign"))
		require.NotContains(t, request.URL.RawQuery, testAppSecret)
		_, err := writer.Write([]byte(`{"code":200,"status":"success","data":{"channels":[2139283,"2139268"],"private":"do not expose"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListChannels(), testConnection, ListChannelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListChannelsBranchListed, result.Branch)
	require.Equal(t, []string{"2139283", "2139268"}, result.Value.ChannelIDs)
}
func TestListChannelsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"bad signature", 200, `{"code":400,"message":"invalid signature.","data":""}`, ListChannelsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"business denial", 200, `{"code":500,"message":"access forbidden","data":""}`, ListChannelsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"business rate limit", 200, `{"code":500,"message":"qps exceeds number of calls","data":""}`, "", sdkgo.FailureRateLimit, true},
		{"unauthorized", 401, `private`, ListChannelsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"server error", 503, `private`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, ListChannelsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing list", 200, `{"code":200,"data":{}}`, ListChannelsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"bad id", 200, `{"code":200,"data":{"channels":["abc"]}}`, ListChannelsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListChannels(), testConnection, ListChannelsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAppSecret)
			}
		})
	}
}
func TestListChannelsBoundAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":200,"data":{"channels":[2139283]}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).ListChannels(), testConnection, ListChannelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListChannelsBranchInvalidResponse, result.Branch)
	client := newTestClient(t, server.URL, 0)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, client.ListChannels(), testConnection, ListChannelsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAppSecret)
}
