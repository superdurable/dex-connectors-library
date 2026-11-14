// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shixiseng

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "shixiseng", Name: "shixiseng"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "shixiseng-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-industries" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{AppID: sdkgo.NewSecretString("merchant-id"), AppSecret: sdkgo.NewSecretString("merchant-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}), WithClock(func() time.Time { return time.Unix(1731510000, 0) }))
	require.NoError(t, err)
	return client
}

func TestMerchantSignatureFixedVector(t *testing.T) {
	require.Equal(t, "54319DD204B503E468D0C75C28500BEA", merchantSignature("merchant-id", "merchant-secret", "1731510000"))
}
func TestListIndustriesSignedRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https", request.URL.Scheme)
		require.Equal(t, "open.shixiseng.com", request.URL.Host)
		require.Equal(t, "/industry/list", request.URL.Path)
		require.Equal(t, merchantSignature("merchant-id", "merchant-secret", "1731510000"), request.URL.Query().Get("sign"))
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("merchant-id:1731510000")), request.Header.Get("Authorization"))
		return reply(200, `{"code":100,"msg":{"industry_list":["互联网/游戏/软件","教育/培训"],"job_category":[],"private":"merchant-secret"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListIndustries(), testConnection, ListIndustriesInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListIndustriesBranchFound, result.Branch)
	require.Equal(t, ListIndustriesOutput{Industries: []string{"互联网/游戏/软件", "教育/培训"}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "merchant-secret")
}
func TestListIndustriesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"auth", 200, `{"code":306,"msg":"merchant-secret"}`, ListIndustriesBranchProviderRejected, false},
		{"temporary", 200, `{"code":300}`, "", true},
		{"throttle", 429, `{}`, "", true},
		{"server", 503, `{}`, "", true},
		{"missing", 200, `{"code":100,"msg":{}}`, ListIndustriesBranchInvalidResponse, false},
		{"wrong type", 200, `{"code":100,"msg":"secret"}`, ListIndustriesBranchInvalidResponse, false},
		{"blank label", 200, `{"code":100,"msg":{"industry_list":[""]}}`, ListIndustriesBranchInvalidResponse, false},
		{"redirect", 302, `{}`, ListIndustriesBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListIndustries(), testConnection, ListIndustriesInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "merchant-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "merchant-secret")
			}
		})
	}
}
func TestListIndustriesTransportAndBounds(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("merchant-secret") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListIndustries(), testConnection, ListIndustriesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "merchant-secret")
	large := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, large, 16).ListIndustries(), testConnection, ListIndustriesInput{})
	require.NoError(t, err)
	require.Equal(t, ListIndustriesBranchInvalidResponse, result.Branch)
}
