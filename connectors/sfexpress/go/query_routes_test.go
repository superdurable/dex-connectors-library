// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sfexpress

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "sfexpress", Name: "sfexpress"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "sfexpress-provider-test" }
func (queryContext) StepExecutionID() string     { return "route-query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PartnerID: "customer-1", Checkword: sdkgo.NewSecretString("private checkword")}})
	require.NoError(t, err)
	return client
}
func runRoutes(t *testing.T, server *httptest.Server) (sdkgo.QueryResult[RouteQueryResult], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).QueryRoutes(), testConnection, QueryRoutesInput{WaybillNumber: "SF1234567890123"})
}
func TestQueryRoutesSignedRequestAndPrivateOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/std/service", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "customer-1", r.PostForm.Get("partnerID"))
		require.Equal(t, "EXP_RECE_SEARCH_ROUTES", r.PostForm.Get("serviceCode"))
		require.Len(t, r.PostForm.Get("requestID"), 36)
		require.Equal(t, 10, len(r.PostForm.Get("timestamp")))
		var message map[string]any
		require.NoError(t, json.Unmarshal([]byte(r.PostForm.Get("msgData")), &message))
		require.Equal(t, []any{"SF1234567890123"}, message["trackingNumber"])
		require.Equal(t, "1", message["trackingType"])
		require.Equal(t, "1", message["methodType"])
		signed := url.QueryEscape(r.PostForm.Get("msgData") + r.PostForm.Get("timestamp") + "private checkword")
		digest := md5.Sum([]byte(signed))
		require.Equal(t, base64.StdEncoding.EncodeToString(digest[:]), r.PostForm.Get("msgDigest"))
		business := `{"success":true,"errorCode":"S0000","msgData":{"routeResps":[{"mailNo":"SF1234567890123","routes":[{"acceptTime":"2026-11-13 09:30:00","acceptAddress":"private city","remark":"已派件","opcode":"50"}]}]}}`
		encoded, _ := json.Marshal(business)
		_, err := fmt.Fprintf(w, `{"apiResultCode":"A1000","apiResultData":%s}`, encoded)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := runRoutes(t, server)
	require.NoError(t, err)
	require.Equal(t, QueryRoutesBranchFound, result.Branch)
	require.Equal(t, "SF1234567890123", result.Value.WaybillNumber)
	require.Equal(t, []RouteEvent{{OccurredAt: "2026-11-13 09:30:00", Opcode: "50"}}, result.Value.Routes)
	encoded, _ := json.Marshal(result.Value)
	require.NotContains(t, string(encoded), "private city")
	require.EqualValues(t, 1, calls.Load())
}
func TestQueryRoutesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejected", 200, `{"apiResultCode":"A1004"}`, QueryRoutesBranchProviderRejected, false},
		{"business rejected", 200, `{"apiResultCode":"A1000","apiResultData":"{\"success\":false,\"errorCode\":\"8013\"}"}`, QueryRoutesBranchProviderRejected, false},
		{"platform limit", 200, `{"apiResultCode":"A1005"}`, "", true},
		{"HTTP limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"bad JSON", 200, `{`, QueryRoutesBranchInvalidResponse, false},
		{"missing route", 200, `{"apiResultCode":"A1000","apiResultData":"{\"success\":true,\"msgData\":{}}"}`, QueryRoutesBranchInvalidResponse, false},
		{"wrong waybill", 200, `{"apiResultCode":"A1000","apiResultData":"{\"success\":true,\"msgData\":{\"routeResps\":[{\"mailNo\":\"OTHER\",\"routes\":[]}]}}"}`, QueryRoutesBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := runRoutes(t, server)
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "private checkword")
				}
			}
		})
	}
}
func TestInvalidWaybillAndCredentialsNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryRoutes(), testConnection, QueryRoutesInput{WaybillNumber: "bad waybill"})
	require.NoError(t, err)
	require.Equal(t, QueryRoutesBranchDefect, result.Branch)
	client, err = New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PartnerID: "customer-1", Checkword: sdkgo.NewSecretString("")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryRoutes(), testConnection, QueryRoutesInput{WaybillNumber: "SF1234567890123"})
	require.NoError(t, err)
	require.Equal(t, QueryRoutesBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardSignedBody(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := runRoutes(t, server)
	require.NoError(t, err)
	require.Equal(t, QueryRoutesBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.False(t, strings.Contains(result.Failure.Message, "private checkword"))
}
