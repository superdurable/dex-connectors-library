// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kuaidi100

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testCustomer = "test-customer"
const testKey = "test-key"

var testConnection = sdkgo.ConnectionRef{Provider: "kuaidi100", Name: "kuaidi100"}
var testInput = QueryTrackingInput{CarrierCode: "yuantong", TrackingNumber: "YT1234567890123", Phone: "13800000000"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "kuaidi100-provider-test" }
func (queryContext) StepExecutionID() string     { return "query-tracking" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Customer: sdkgo.NewSecretString(testCustomer), Key: sdkgo.NewSecretString(testKey)}})
	require.NoError(t, err)
	return client
}

func TestTrackingSignedRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/poll/query.do", request.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, testCustomer, request.PostForm.Get("customer"))
		param := request.PostForm.Get("param")
		var payload map[string]string
		require.NoError(t, json.Unmarshal([]byte(param), &payload))
		require.Equal(t, "yuantong", payload["com"])
		require.Equal(t, "YT1234567890123", payload["num"])
		require.Equal(t, "13800000000", payload["phone"])
		require.Equal(t, "0", payload["show"])
		digest := md5.Sum([]byte(param + testKey + testCustomer))
		require.Equal(t, strings.ToUpper(hex.EncodeToString(digest[:])), request.PostForm.Get("sign"))
		_, err := writer.Write([]byte(`{"message":"ok","status":"200","nu":"YT1234567890123","com":"yuantong","state":"0","data":[{"time":"2026-11-13 10:00:00","context":"Arrived"},{"time":"2026-11-12 15:30:00","context":"Collected"}],"courierInfo":{"pickupManPhone":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).QueryTracking(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, QueryTrackingBranchRetrieved, result.Branch)
	require.Equal(t, QueryTrackingOutput{CarrierCode: "yuantong", TrackingNumber: "YT1234567890123", State: "0", Events: []TrackingEvent{{Time: "2026-11-13 10:00:00", Content: "Arrived"}, {Time: "2026-11-12 15:30:00", Content: "Collected"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}

func TestTrackingOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"not found", 200, `{"result":false,"returnCode":"500","message":"private waybill"}`, QueryTrackingBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"bad signature", 200, `{"result":false,"returnCode":"503","message":"private auth"}`, QueryTrackingBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"locked", 200, `{"result":false,"returnCode":"504","message":"private lock"}`, QueryTrackingBranchProviderRejected, sdkgo.FailureRateLimit, false},
		{"provider busy", 200, `{"result":false,"returnCode":"502","message":"private busy"}`, "", sdkgo.FailureAvailability, true},
		{"http rate limit", 429, `{}`, "", sdkgo.FailureRateLimit, true},
		{"http unavailable", 503, `{}`, "", sdkgo.FailureAvailability, true},
		{"mismatched waybill", 200, `{"nu":"OTHER123","com":"yuantong","status":"200","state":"0","data":[]}`, QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"malformed", 200, `{`, QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).QueryTracking(), testConnection, testInput)
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 30*time.Minute, retry.After)
				require.NotContains(t, err.Error(), testKey)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
			require.Equal(t, tc.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "private")
		})
	}
}

func TestTrackingInputValidationAndBoundedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"nu":"YT1234567890123","com":"yuantong","state":"0","data":[]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	invalid := testInput
	invalid.TrackingNumber = "short"
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryTracking(), testConnection, invalid)
	require.NoError(t, err)
	require.Equal(t, QueryTrackingBranchDefect, result.Branch)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Customer: sdkgo.NewSecretString(testCustomer), Key: sdkgo.NewSecretString(testKey)}}
	bounded, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, bounded.QueryTracking(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, QueryTrackingBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, client.QueryTracking(), testConnection, testInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
