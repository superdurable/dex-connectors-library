// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ctyun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessKey = "AKIDEXAMPLE"
const testSecurityKey = "SKEXAMPLE"
const testTenantID = "tenant-example"

var testConnection = sdkgo.ConnectionRef{Provider: "ctyun", Name: "ctyun"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "ctyun-provider-test" }
func (queryContext) StepExecutionID() string     { return "knowledge-base-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ serverURL *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = transport.serverURL.Scheme
	cloned.URL.Host = transport.serverURL.Host
	return http.DefaultTransport.RoundTrip(cloned)
}
func newTestClient(t *testing.T, server *httptest.Server, maxResponseBytes int64) *Client {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKey: sdkgo.NewSecretString(testAccessKey), SecurityKey: sdkgo.NewSecretString(testSecurityKey), TenantID: sdkgo.NewSecretString(testTenantID)}}
	client, err := New(Config{BaseURL: "https://kqa-global.ctapi.ctyun.cn", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestCTYunSignatureFixedVector(t *testing.T) {
	require.Equal(t, "AKIDEXAMPLE headers=ctyun-eop-request-id;eop-date Signature=SSs71iiml5WzAzRleNbbKLs2zyt4Oixio16KWIsAdgY=", signAuthorization(testAccessKey, testSecurityKey, "123456789", "20211221T163614Z"))
}
func TestListKnowledgeBasesSignedRequestAndPrivacy(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, knowledgeBasesPath, request.URL.Path)
		require.Equal(t, testTenantID, request.Header.Get("tenantId"))
		requestID := request.Header.Get("ctyun-eop-request-id")
		require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), requestID)
		date := request.Header.Get("eop-date")
		require.Regexp(t, `^[0-9]{8}T[0-9]{6}Z$`, date)
		require.Equal(t, signAuthorization(testAccessKey, testSecurityKey, requestID, date), request.Header.Get("Eop-Authorization"))
		require.NotContains(t, request.URL.String(), testSecurityKey)
		_, err := writer.Write([]byte(`{"statusCode":"200","message":"Success","returnObj":[{"id":280,"name":"Example knowledge base","count":4,"private":"omitted"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListKnowledgeBases(), testConnection, ListKnowledgeBasesInput{})
	require.NoError(t, err)
	require.Equal(t, ListKnowledgeBasesBranchListed, result.Branch)
	require.Equal(t, []KnowledgeBaseSummary{{ID: 280, Name: "Example knowledge base", FileCount: 4}}, result.Value.KnowledgeBases)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "omitted")
	require.NotContains(t, string(encoded), testSecurityKey)
	require.Equal(t, 1, requests)
}
func TestListKnowledgeBasesFailureClasses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"invalid key", 403, "private", ListKnowledgeBasesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"rate limit", 429, "private", "", sdkgo.FailureRateLimit, true},
		{"server outage", 503, "private", "", sdkgo.FailureAvailability, true},
		{"business auth", 200, `{"statusCode":"40003","error":"KQA_40003","message":"private"}`, ListKnowledgeBasesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"business temporary", 200, `{"statusCode":"40004","error":"KQA_40004","message":"private"}`, "", sdkgo.FailureAvailability, true},
		{"business rejection", 200, `{"statusCode":"40005","error":"KQA_40005","message":"private"}`, ListKnowledgeBasesBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"missing list", 200, `{"statusCode":200}`, ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"empty list", 200, `{"statusCode":200,"returnObj":[]}`, ListKnowledgeBasesBranchListed, "", false},
		{"bad item", 200, `{"statusCode":200,"returnObj":[{"name":"x"}]}`, ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListKnowledgeBases(), testConnection, ListKnowledgeBasesInput{})
			if tc.retry {
				if tc.status == 429 {
					var retry *dex.RetryAfterError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, 2*time.Second, retry.After)
				} else {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if tc.kind != "" {
					require.Equal(t, tc.kind, result.Failure.Kind)
					require.NotContains(t, result.Failure.Message, "private")
				}
			}
		})
	}
}
func TestListKnowledgeBasesTransportAndURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"statusCode":200,"returnObj":[]}`))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server, 0)
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.ListKnowledgeBases(), testConnection, ListKnowledgeBasesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKey: sdkgo.NewSecretString(testAccessKey), SecurityKey: sdkgo.NewSecretString(testSecurityKey), TenantID: sdkgo.NewSecretString(testTenantID)}}
	_, err = New(Config{BaseURL: "http://kqa-global.ctapi.ctyun.cn"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
