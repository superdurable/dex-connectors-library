// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qince

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testOpenID = "8519670967275282420"
const testAppKey = "2EV5i30kYI6r7Hz817"

var testConnection = sdkgo.ConnectionRef{Provider: "qince", Name: "qince"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "qince-provider-test" }
func (queryContext) StepExecutionID() string     { return "department-list" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {OpenID: testOpenID, AppKey: sdkgo.NewSecretString(testAppKey)}}
	client, err := New(Config{BaseURL: "https://openapi.region2.qince.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{destination: destination}}))
	require.NoError(t, err)
	return client
}
func TestQinceOfficialSignatureVector(t *testing.T) {
	require.Equal(t, "b7b7d9810bbb0786e9560751dc2b6597", signDepartmentQuery(`{"org_status":"1"}`, testAppKey, "1617206400000"))
}
func TestListDepartmentsSignedRequestAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		segments := strings.Split(strings.TrimPrefix(request.URL.Path, departmentQueryPath), "/")
		require.Len(t, segments, 4)
		require.Equal(t, testOpenID, segments[0])
		require.Len(t, segments[1], 13)
		require.Len(t, segments[3], 32)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body json.RawMessage
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, departmentQueryBody, string(body))
		require.Equal(t, signDepartmentQuery(string(body), testAppKey, segments[1]), segments[2])
		require.NotContains(t, request.URL.String(), testAppKey)
		responseData := `[{"id":"10","org_code":"DD001","org_name":"市场部","org_status":"1","person_phone":"secret"}]`
		response, _ := json.Marshal(map[string]any{"return_code": "0", "msg_id": segments[3], "response_data": responseData, "email": "private"})
		_, err := writer.Write(response)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDepartments(), testConnection, ListDepartmentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListDepartmentsBranchListed, result.Branch)
	require.Equal(t, ListDepartmentsOutput{Departments: []Department{{ID: "10", Code: "DD001", Name: "市场部", Active: true}}}, result.Value)
}

func TestListDepartmentsAcceptsArrayResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		segments := strings.Split(request.URL.Path, "/")
		body, err := json.Marshal(map[string]any{
			"return_code":   0,
			"msg_id":        segments[len(segments)-1],
			"response_data": []map[string]any{{"id": "10", "org_code": "DD001", "org_name": "研发中心", "org_status": "1"}},
		})
		require.NoError(t, err)
		_, err = writer.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDepartments(), testConnection, ListDepartmentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListDepartmentsBranchListed, result.Branch)
	require.Equal(t, []Department{{ID: "10", Code: "DD001", Name: "研发中心", Active: true}}, result.Value.Departments)
}
func TestListDepartmentsOutcomeClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, `{"return_code":"1"}`, ListDepartmentsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `{"return_code":"1"}`, ListDepartmentsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"provider rejected", 200, `{"return_code":"100103"}`, ListDepartmentsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"temporary", 503, `private`, "", sdkgo.FailureAvailability, true},
		{"invalid json", 200, `{`, ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"bad data", 200, `{"return_code":"0","response_data":"{}"}`, ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				body := tc.body
				if tc.status == 200 && strings.Contains(body, "return_code") {
					segments := strings.Split(request.URL.Path, "/")
					body = strings.TrimSuffix(body, "}") + `,"msg_id":"` + segments[len(segments)-1] + `"}`
				}
				_, err := writer.Write([]byte(body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDepartments(), testConnection, ListDepartmentsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAppKey)
			}
		})
	}
}
func TestQinceBaseURLValidation(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	for _, origin := range []string{"http://openapi.qince.com", "https://other.example.com", "https://openapi.qince.com/api"} {
		_, err := New(Config{BaseURL: origin}, credentials)
		require.Error(t, err, origin)
	}
}
func TestListDepartmentsBoundAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(strings.Repeat("x", 100)))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).ListDepartments(), testConnection, ListDepartmentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListDepartmentsBranchInvalidResponse, result.Branch)
	client := newTestClient(t, server.URL, 0)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, client.ListDepartments(), testConnection, ListDepartmentsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAppKey)
}
func TestQinceSignatureUsesRawBody(t *testing.T) {
	body := "{\"org_status\":\"1\"}"
	sum := md5.Sum([]byte(body + "|" + testAppKey + "|1617206400000"))
	require.Equal(t, hex.EncodeToString(sum[:]), signDepartmentQuery(body, testAppKey, "1617206400000"))
}
