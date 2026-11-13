// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package okki

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "okki", Name: "okki"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "okki-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-customer-fields" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credentials := Credentials{ClientID: "client123", ClientSecret: sdkgo.NewSecretString("secretKey"), AccessToken: sdkgo.NewSecretString("jwt123")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestListCustomerFieldsUsesRawAuthorizationAndExcludesDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/company/fields", request.URL.Path)
		require.Equal(t, "company", request.URL.Query().Get("type"))
		require.Equal(t, "jwt123", request.Header.Get("Authorization"))
		require.NotContains(t, request.URL.String(), "secretKey")
		_, err := io.WriteString(writer, `{"code":200,"message":"success","data":[{"id":"origin_list","name":"客户来源","field_type":7,"default":"private@example.com","ext_info":["private"]},{"id":5200812401,"name":"测试日期","field_type":4,"default":"private"},{"id":"","name":"来源详情","field_type":7}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListCustomerFields(), testConnection, ListCustomerFieldsInput{})
	require.NoError(t, err)
	require.Equal(t, ListCustomerFieldsBranchListed, result.Branch)
	require.Equal(t, CustomerFields{Fields: []CustomerField{{ID: "origin_list", Name: "客户来源", FieldType: 7}, {ID: "5200812401", Name: "测试日期", FieldType: 4}}}, result.Value)
}
func TestListCustomerFieldsClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{{"rejected", 200, `{"code":403,"message":"no permission"}`, ListCustomerFieldsBranchProviderRejected, false}, {"provider unavailable", 200, `{"code":500,"message":"unavailable"}`, "", true}, {"unauthorized", 401, `{}`, ListCustomerFieldsBranchProviderRejected, false}, {"rate limited", 429, `{}`, "", true}, {"unavailable", 503, `{}`, "", true}, {"missing data", 200, `{"code":200}`, ListCustomerFieldsBranchInvalidResponse, false}, {"missing id", 200, `{"code":200,"data":[{"name":"Name","field_type":1}]}`, ListCustomerFieldsBranchInvalidResponse, false}, {"missing type", 200, `{"code":200,"data":[{"id":"name","name":"Name"}]}`, ListCustomerFieldsBranchInvalidResponse, false}, {"malformed", 200, `{"code":`, ListCustomerFieldsBranchInvalidResponse, false}, {"trailing", 200, `{"code":200,"data":[]} {}`, ListCustomerFieldsBranchInvalidResponse, false}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListCustomerFields(), testConnection, ListCustomerFieldsInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "secretKey")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestListCustomerFieldsTransportCredentialsAndBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).ListCustomerFields(), testConnection, ListCustomerFieldsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListCustomerFields(), testConnection, ListCustomerFieldsInput{})
	require.NoError(t, err)
	require.Equal(t, ListCustomerFieldsBranchDefect, result.Branch)
	_, err = New(Config{BaseURL: "https://api-sandbox.xiaoman.cn/evil?secret=x"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"code":200,"data":[]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).ListCustomerFields(), testConnection, ListCustomerFieldsInput{})
	require.NoError(t, err)
	require.Equal(t, ListCustomerFieldsBranchInvalidResponse, result.Branch)
	require.False(t, strings.Contains(result.Failure.Message, "secretKey"))
}

type refreshingTestCredentials struct{ before, after Credentials }

func (source *refreshingTestCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return source.before, nil
}
func (source *refreshingTestCredentials) ResolveWithRefresh(_ context.Context, _ sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return source.before, nil
}
func (source *refreshingTestCredentials) ResolveAfterRejection(_ context.Context, _ sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return source.after, nil
}
func TestListCustomerFieldsRenewsRejectedTokenOnce(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			require.Equal(t, "expired", request.Header.Get("Authorization"))
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		require.Equal(t, "fresh", request.Header.Get("Authorization"))
		_, err := io.WriteString(writer, `{"code":200,"data":[]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	source := &refreshingTestCredentials{before: Credentials{ClientID: "client123", ClientSecret: sdkgo.NewSecretString("secretKey"), AccessToken: sdkgo.NewSecretString("expired")}, after: Credentials{ClientID: "client123", ClientSecret: sdkgo.NewSecretString("secretKey"), AccessToken: sdkgo.NewSecretString("fresh")}}
	client, err := New(Config{BaseURL: server.URL}, source)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListCustomerFields(), testConnection, ListCustomerFieldsInput{})
	require.NoError(t, err)
	require.Equal(t, ListCustomerFieldsBranchListed, result.Branch)
	require.Equal(t, 2, requests)
}
