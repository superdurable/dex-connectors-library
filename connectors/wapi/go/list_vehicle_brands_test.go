// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wapi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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

var testConnection = sdkgo.ConnectionRef{Provider: "wapi", Name: "wapi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "wapi-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-brands" }
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
	credentials := Credentials{AppID: "123", AppSecret: sdkgo.NewSecretString("0123456789abcdef0123456789abcdef")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListVehicleBrandsRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, brandPriceEndpoint, request.URL.Scheme+"://"+request.URL.Host+request.URL.Path)
		query := request.URL.Query()
		require.Equal(t, "123", query.Get("appid"))
		require.Equal(t, "json", query.Get("format"))
		require.Equal(t, "", request.Header.Get("Authorization"))
		require.NotEmpty(t, query.Get("time"))
		digest := md5.Sum([]byte("appid123formatjsontime" + query.Get("time") + "0123456789abcdef0123456789abcdef"))
		require.Equal(t, hex.EncodeToString(digest[:]), query.Get("sign"))
		return reply(200, `{"codeid":"10000","retdata":[{"ppid":"15","pp_name":"宝马","pp_price":3400,"private":"secret"}],"message":"secret"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListVehicleBrands(), testConnection, ListVehicleBrandsInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListVehicleBrandsBranchFound, result.Branch)
	require.Equal(t, ListVehicleBrandsOutput{Total: 1, Brands: []VehicleBrandPrice{{ID: "15", Name: "宝马", PriceFen: 3400}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
}
func TestListVehicleBrandsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"codeid":"10000","retdata":[]}`, ListVehicleBrandsBranchFound, false},
		{"bad key", 401, `{"message":"secret"}`, ListVehicleBrandsBranchProviderRejected, false},
		{"forbidden", 403, `{"message":"secret"}`, ListVehicleBrandsBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true}, {"server", 503, `{}`, "", true},
		{"business reject", 200, `{"codeid":"10003","message":"secret"}`, ListVehicleBrandsBranchProviderRejected, false},
		{"provider error", 200, `{"codeid":"10021"}`, "", true},
		{"missing list", 200, `{"codeid":"10000"}`, ListVehicleBrandsBranchInvalidResponse, false},
		{"bad price", 200, `{"codeid":"10000","retdata":[{"ppid":"15","pp_name":"宝马","pp_price":-1}]}`, ListVehicleBrandsBranchInvalidResponse, false},
		{"redirect", 302, `{}`, ListVehicleBrandsBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListVehicleBrands(), testConnection, ListVehicleBrandsInput{})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "0123456789abcdef")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
		})
	}
}
func TestListVehicleBrandsOversizeAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListVehicleBrands(), testConnection, ListVehicleBrandsInput{})
	require.NoError(t, err)
	require.Equal(t, ListVehicleBrandsBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListVehicleBrands(), testConnection, ListVehicleBrandsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "secret")
}
