// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package submail

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "submail", Name: "submail"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "submail-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-sms-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{AppID: "test-app-id", AppKey: sdkgo.NewSecretString("test-app-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestGetSMSBalanceUsesNormalAuthorizationInForm(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/balance/sms.json", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.Empty(t, request.Header.Get("Authorization"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, "test-app-id", request.PostForm.Get("appid"))
		require.Equal(t, "test-app-key", request.PostForm.Get("signature"))
		require.Empty(t, request.PostForm.Get("timestamp"))
		require.Empty(t, request.PostForm.Get("sign_type"))
		_, err := io.WriteString(writer, `{"status":"success","balance":"9410","transactional_balance":"9775"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetSMSBalance(), testConnection, GetSMSBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetSMSBalanceBranchFound, result.Branch)
	require.Equal(t, SMSBalance{GeneralCredits: "9410", TransactionalCredits: "9775"}, result.Value)
}
func TestGetSMSBalanceClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"zero balance", 200, `{"status":"success","balance":"0","transactional_balance":"0"}`, GetSMSBalanceBranchFound, false},
		{"provider error", 200, `{"status":"error","code":"1201","msg":"bad app"}`, GetSMSBalanceBranchProviderRejected, false},
		{"unauthorized", 401, `{"status":"error","code":"1202"}`, GetSMSBalanceBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing balance", 200, `{"status":"success"}`, GetSMSBalanceBranchInvalidResponse, false},
		{"negative balance", 200, `{"status":"success","balance":"-1","transactional_balance":"3"}`, GetSMSBalanceBranchInvalidResponse, false},
		{"number instead of string", 200, `{"status":"success","balance":10,"transactional_balance":"3"}`, GetSMSBalanceBranchInvalidResponse, false},
		{"unknown status", 200, `{"status":"other"}`, GetSMSBalanceBranchInvalidResponse, false},
		{"malformed", 200, `{"status":`, GetSMSBalanceBranchInvalidResponse, false},
		{"trailing", 200, `{"status":"success","balance":"1","transactional_balance":"2"}{}`, GetSMSBalanceBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetSMSBalance(), testConnection, GetSMSBalanceInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetSMSBalanceBoundsAndCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"status":"success","balance":"1","transactional_balance":"2"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).GetSMSBalance(), testConnection, GetSMSBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetSMSBalanceBranchInvalidResponse, result.Branch)
	endpoint := server.URL
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).GetSMSBalance(), testConnection, GetSMSBalanceInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetSMSBalance(), testConnection, GetSMSBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetSMSBalanceBranchDefect, result.Branch)
	_, err = New(Config{BaseURL: "https://api-v4.mysubmail.com/balance/sms.json"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	for _, value := range []string{"", "bad name", "bad\nname"} {
		require.False(t, validAppID(value))
	}
}
