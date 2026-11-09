// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "adobe-acrobat-sign", Name: "adobe-acrobat-sign"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "acrobatsign-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-agreement" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{IntegrationKey: sdkgo.NewSecretString("sample-acrobat-sign-integration-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetAgreementReadsSelectedFields(t *testing.T) {
	var server *httptest.Server
	discoveryCount := 0
	lookupCount := 0
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "Bearer sample-acrobat-sign-integration-key", request.Header.Get("Authorization"))
		if request.URL.Path == "/api/rest/v6/baseUris" {
			discoveryCount++
			_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
			require.NoError(t, err)
			return
		}
		lookupCount++
		require.Equal(t, "/api/rest/v6/agreements/3AAABLblqZNOTREALAGREEMENTID5_BjiH", request.URL.Path)
		_, err := writer.Write([]byte(`{"id":"3AAABLblqZNOTREALAGREEMENTID5_BjiH","name":"Example agreement","status":"SIGNED","participantSetsInfo":[{"memberInfos":[{"email":"private@example.com"}]}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/rest/v6", 0)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetAgreement(), testConnection, GetAgreementInput{AgreementID: "3AAABLblqZNOTREALAGREEMENTID5_BjiH"})
		require.NoError(t, err)
		require.Equal(t, GetAgreementBranchFound, result.Branch)
		require.Equal(t, AgreementStatus{ID: "3AAABLblqZNOTREALAGREEMENTID5_BjiH", Name: "Example agreement", Status: "SIGNED"}, result.Value)
	}
	require.Equal(t, 1, discoveryCount)
	require.Equal(t, 2, lookupCount)
}

func TestGetAgreementClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		status    int
		body      string
		branch    sdkgo.BranchID
		retry     bool
		discovery bool
	}{
		{name: "missing", status: 404, branch: GetAgreementBranchNotFound},
		{name: "forbidden", status: 403, branch: GetAgreementBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"3AAABLblqZNOTREALAGREEMENTID5_BjiH","name":"Example agreement"}`, branch: GetAgreementBranchInvalidResponse},
		{name: "discovery rejected", status: 403, branch: GetAgreementBranchProviderRejected, discovery: true},
		{name: "discovery rate limit", status: 429, retry: true, discovery: true},
		{name: "discovery unavailable", status: 503, retry: true, discovery: true},
		{name: "unsafe access point", status: 200, body: `{"apiAccessPoint":"https://evil.example/"}`, branch: GetAgreementBranchInvalidResponse, discovery: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/rest/v6/baseUris" && !testCase.discovery {
					_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
					require.NoError(t, err)
					return
				}
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api/rest/v6", 0).GetAgreement(), testConnection, GetAgreementInput{AgreementID: "3AAABLblqZNOTREALAGREEMENTID5_BjiH"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestValidateAccessPointRejectsForeignHosts(t *testing.T) {
	for _, accessPoint := range []string{"https://evil.example/", "http://169.254.169.254/", "https://api.na4.adobesign.com/extra", "https://user@api.na4.adobesign.com/"} {
		_, err := validateAccessPoint(accessPoint, "https://api.adobesign.com/api/rest/v6")
		require.Error(t, err)
	}
	accessPoint, err := validateAccessPoint("https://api.na4.adobesign.com/", "https://api.adobesign.com/api/rest/v6")
	require.NoError(t, err)
	require.Equal(t, "https://api.na4.adobesign.com", accessPoint)
}

func TestGetAgreementRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.adobesign.com/api/rest/v6", 0)
	for _, agreementID := range []string{"", "../other", "bad\nagreement", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetAgreement(), testConnection, GetAgreementInput{AgreementID: agreementID})
		require.NoError(t, err)
		require.Equal(t, GetAgreementBranchDefect, result.Branch)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/rest/v6/baseUris" {
			_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
			require.NoError(t, err)
			return
		}
		_, err := writer.Write([]byte(`{"id":"3AAABLblqZNOTREALAGREEMENTID5_BjiH","name":"Example agreement","status":"SIGNED","extra":"oversized"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api/rest/v6", 80).GetAgreement(), testConnection, GetAgreementInput{AgreementID: "3AAABLblqZNOTREALAGREEMENTID5_BjiH"})
	require.NoError(t, err)
	require.Equal(t, GetAgreementBranchInvalidResponse, result.Branch)
}
