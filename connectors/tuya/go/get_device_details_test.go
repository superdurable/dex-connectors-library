// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tuya

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "tuya", Name: "tuya"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tuya-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-device-details" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{ClientID: sdkgo.NewSecretString("sample-client-id"), ClientSecret: sdkgo.NewSecretString("sample-client-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestTuyaSignatureOfficialFixedVectors(t *testing.T) {
	headers := "area_id:29a33e8796834b1efa6\ncall_id:8afdb70ab2ed11eb85290242ac130003\n"
	require.Equal(t, "9E48A3E93B302EEECC803C7241985D0A34EB944F40FB573C7B5C2A82158AF13E", tuyaSignature("1KAD46OrT9HafiKdsXeg", "4OHBOnWOqaEC1mWXOpVL3yV50s0qGSRC", "", "1588925778000", "5138cc3a9033d69856923fd07b491173", headers, "/v1.0/token?grant_type=1"))
	require.Equal(t, "AE4481C692AA80B25F3A7E12C3A5FD9BBF6251539DD78E565A1A72A508A88784", tuyaSignature("1KAD46OrT9HafiKdsXeg", "4OHBOnWOqaEC1mWXOpVL3yV50s0qGSRC", "3f4eda2bdec17232f67c0b188af3eec1", "1588925778000", "5138cc3a9033d69856923fd07b491173", headers, "/v2.0/apps/schema/users?page_no=1&page_size=50"))
}
func TestGetDeviceDetailsSignsTokenAndDeviceWithoutLeakingLocalKey(t *testing.T) {
	calls := 0
	nonces := map[string]bool{}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://openapi.tuyacn.com", request.URL.Scheme+"://"+request.URL.Host)
		require.Equal(t, "sample-client-id", request.Header.Get("client_id"))
		require.Equal(t, "HMAC-SHA256", request.Header.Get("sign_method"))
		timestamp := request.Header.Get("t")
		require.Len(t, timestamp, 13)
		nonce := request.Header.Get("nonce")
		require.Len(t, nonce, 32)
		require.False(t, nonces[nonce])
		nonces[nonce] = true
		accessToken := request.Header.Get("access_token")
		expected := tuyaSignature("sample-client-id", "sample-client-secret", accessToken, timestamp, nonce, "", request.URL.RequestURI())
		require.Equal(t, expected, request.Header.Get("sign"))
		if calls == 1 {
			require.Equal(t, tokenPath, request.URL.RequestURI())
			require.Empty(t, accessToken)
			return response(200, `{"success":true,"result":{"access_token":"sample-access-token","expire_time":7200,"refresh_token":"private-refresh-token"}}`), nil
		}
		require.Equal(t, "/v1.0/devices/device123", request.URL.Path)
		require.Equal(t, "sample-access-token", accessToken)
		return response(200, `{"success":true,"result":{"id":"device123","name":"Body fat scale","online":true,"category":"qt","product_id":"product42","create_time":1560827137,"update_time":1589764585,"local_key":"private-device-key","status":[{"code":"password","value":"private-dp-value"}]}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetDeviceDetails(), testConnection, GetDeviceDetailsInput{DeviceID: "device123"})
	require.NoError(t, err)
	require.Equal(t, GetDeviceDetailsBranchFound, result.Branch)
	require.Equal(t, GetDeviceDetailsOutput{DeviceID: "device123", Name: "Body fat scale", Online: true, Category: "qt", ProductID: "product42", CreatedAtUnix: 1560827137, UpdatedAtUnix: 1589764585}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-device-key")
	require.NotContains(t, string(encoded), "private-dp-value")
	require.NotContains(t, string(encoded), "sample-access-token")
	require.Equal(t, 2, calls)
}
func TestGetDeviceDetailsClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		tokenStatus  int
		tokenBody    string
		deviceStatus int
		deviceBody   string
		branch       sdkgo.BranchID
		retry        bool
		count        int
	}{
		{"token rejected", 200, `{"success":false,"code":1001,"msg":"private-client-secret"}`, 0, "", GetDeviceDetailsBranchProviderRejected, false, 1},
		{"token malformed", 200, `{"success":true,"result":{}}`, 0, "", GetDeviceDetailsBranchInvalidResponse, false, 1},
		{"token rate limited", 429, "", 0, "", "", true, 1},
		{"token system error", 200, `{"success":false,"code":500}`, 0, "", "", true, 1},
		{"device missing", 200, `{"success":true,"result":{"access_token":"sample-access-token","expire_time":7200}}`, 200, `{"success":false,"code":1000}`, GetDeviceDetailsBranchNotFound, false, 2},
		{"device forbidden", 200, `{"success":true,"result":{"access_token":"sample-access-token","expire_time":7200}}`, 403, "", GetDeviceDetailsBranchProviderRejected, false, 2},
		{"device unavailable", 200, `{"success":true,"result":{"access_token":"sample-access-token","expire_time":7200}}`, 503, "", "", true, 2},
		{"device token expired", 200, `{"success":true,"result":{"access_token":"sample-access-token","expire_time":7200}}`, 200, `{"success":false,"code":1010}`, "", true, 2},
		{"device mismatch", 200, `{"success":true,"result":{"access_token":"sample-access-token","expire_time":7200}}`, 200, `{"success":true,"result":{"id":"other","name":"Other","online":true}}`, GetDeviceDetailsBranchInvalidResponse, false, 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return response(testCase.tokenStatus, testCase.tokenBody), nil
				}
				return response(testCase.deviceStatus, testCase.deviceBody), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetDeviceDetails(), testConnection, GetDeviceDetailsInput{DeviceID: "device123"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.branch, result.Branch)
				require.NotContains(t, result.Failure.Message, "private-client-secret")
			}
			require.Equal(t, testCase.count, calls)
		})
	}
}
func TestGetDeviceDetailsRejectsInvalidInputAndBoundsResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, strings.Repeat("x", 200)), nil
	})
	client := newTestClient(t, transport, 64)
	for _, id := range []string{"", "../device", "bad/device", "bad?query", strings.Repeat("x", 129)} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetDeviceDetails(), testConnection, GetDeviceDetailsInput{DeviceID: id})
		require.NoError(t, err)
		require.Equal(t, GetDeviceDetailsBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetDeviceDetails(), testConnection, GetDeviceDetailsInput{DeviceID: "device123"})
	require.NoError(t, err)
	require.Equal(t, GetDeviceDetailsBranchInvalidResponse, result.Branch)
	require.Equal(t, 1, calls)
}
func TestGetDeviceDetailsTransportErrorIsSecretSafe(t *testing.T) {
	client := newTestClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("sample-client-secret") }), 0)
	_, err := sdkgo.RunQuery(queryContext{}, client.GetDeviceDetails(), testConnection, GetDeviceDetailsInput{DeviceID: "device123"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "sample-client-secret")
}

func TestTokenRedirectDoesNotForwardProjectCredentials(t *testing.T) {
	calls := 0
	client := newTestClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "openapi.tuyacn.com", request.URL.Host)
		redirect := response(http.StatusFound, "")
		redirect.Header.Set("Location", "https://other.example/v1.0/token")
		return redirect, nil
	}), 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetDeviceDetails(), testConnection, GetDeviceDetailsInput{DeviceID: "device123"})
	require.NoError(t, err)
	require.Equal(t, GetDeviceDetailsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
