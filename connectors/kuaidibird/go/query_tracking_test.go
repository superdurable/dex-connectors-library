// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kuaidibird

import (
	"context"
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

const testBusinessID = "1237100"
const testAPIKey = "test-api-key"

var testConnection = sdkgo.ConnectionRef{Provider: "kuaidibird", Name: "kuaidibird"}
var testInput = QueryTrackingInput{CarrierCode: "YTO", TrackingNumber: "YT1234567890123"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "kuaidibird-provider-test" }
func (queryContext) StepExecutionID() string     { return "query-tracking" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {BusinessID: sdkgo.NewSecretString(testBusinessID), APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func TestPublishedSignatureVector(t *testing.T) {
	require.Equal(t, "OWFhM2I5N2ViM2U2MGRkMjc4YzU2NmVlZWI3ZDk0MmE=", signTrackingRequest("{'OrderCode':'','ShipperCode':'SF','LogisticCode':'118954907573'}", "56da2cf8-c8a2-44b2-b6fa-476cd7d1ba17"))
}

func TestTrackingSignedRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/Ebusiness/EbusinessOrderHandle.aspx", request.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded;charset=utf-8", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, testBusinessID, request.PostForm.Get("EBusinessID"))
		require.Equal(t, "1002", request.PostForm.Get("RequestType"))
		require.Equal(t, "2", request.PostForm.Get("DataType"))
		requestData := request.PostForm.Get("RequestData")
		var payload map[string]string
		require.NoError(t, json.Unmarshal([]byte(requestData), &payload))
		require.Equal(t, "YTO", payload["ShipperCode"])
		require.Equal(t, "YT1234567890123", payload["LogisticCode"])
		require.Equal(t, signTrackingRequest(requestData, testAPIKey), request.PostForm.Get("DataSign"))
		_, err := writer.Write([]byte(`{"EBusinessID":"1237100","ShipperCode":"YTO","LogisticCode":"YT1234567890123","Success":true,"State":2,"Traces":[{"AcceptTime":"2026-11-12 15:30:00","AcceptStation":"Collected"},{"AcceptTime":"2026-11-13 10:00:00","AcceptStation":"Arrived"}],"Reason":null}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).QueryTracking(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, QueryTrackingBranchRetrieved, result.Branch)
	require.Equal(t, QueryTrackingOutput{CarrierCode: "YTO", TrackingNumber: "YT1234567890123", State: "2", Events: []TrackingEvent{{Time: "2026-11-12 15:30:00", Content: "Collected"}, {Time: "2026-11-13 10:00:00", Content: "Arrived"}}}, result.Value)
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
		{"no tracking", 200, `{"EBusinessID":"1237100","Success":false,"Reason":"private waybill"}`, QueryTrackingBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"http unauthorized", 401, `{"Success":false,"Reason":"private auth"}`, QueryTrackingBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"http rate limit", 429, `{}`, "", sdkgo.FailureRateLimit, true},
		{"http unavailable", 503, `{}`, "", sdkgo.FailureAvailability, true},
		{"mismatched waybill", 200, `{"EBusinessID":"1237100","Success":true,"ShipperCode":"YTO","LogisticCode":"OTHER123","State":"2","Traces":[]}`, QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing state", 200, `{"EBusinessID":"1237100","Success":true,"ShipperCode":"YTO","LogisticCode":"YT1234567890123","Traces":[]}`, QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"malformed", 200, `{`, QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "35")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).QueryTracking(), testConnection, testInput)
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 35*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAPIKey)
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
		_, err := writer.Write([]byte(`{"EBusinessID":"1237100","Success":true,"ShipperCode":"YTO","LogisticCode":"YT1234567890123","State":"2","Traces":[]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	invalid := testInput
	invalid.CarrierCode = "yto"
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryTracking(), testConnection, invalid)
	require.NoError(t, err)
	require.Equal(t, QueryTrackingBranchDefect, result.Branch)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {BusinessID: sdkgo.NewSecretString(testBusinessID), APIKey: sdkgo.NewSecretString(testAPIKey)}}
	bounded, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, bounded.QueryTracking(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, QueryTrackingBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, client.QueryTracking(), testConnection, testInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, strings.ToLower(err.Error()), "test-api-key")
}
