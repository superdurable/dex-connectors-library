// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropboxsign

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "dropbox-sign", Name: "dropbox-sign"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "dropboxsign-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-signature-request" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-dropbox-sign-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetSignatureRequestReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/signature_request/fa5c8a0b0f492d768749333ad6fcc214c111e967", request.URL.Path)
		username, password, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "sample-dropbox-sign-api-key", username)
		require.Empty(t, password)
		_, err := writer.Write([]byte(`{"signature_request":{"signature_request_id":"fa5c8a0b0f492d768749333ad6fcc214c111e967","title":"Example agreement","is_complete":true,"is_declined":false,"has_error":false,"signatures":[{"signer_email_address":"private@example.com"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetSignatureRequest(), testConnection, GetSignatureRequestInput{SignatureRequestID: "fa5c8a0b0f492d768749333ad6fcc214c111e967"})
	require.NoError(t, err)
	require.Equal(t, GetSignatureRequestBranchFound, result.Branch)
	require.Equal(t, SignatureRequestStatus{ID: "fa5c8a0b0f492d768749333ad6fcc214c111e967", Title: "Example agreement", IsComplete: true}, result.Value)
}

func TestGetSignatureRequestClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetSignatureRequestBranchNotFound},
		{name: "forbidden", status: 403, branch: GetSignatureRequestBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"signature_request":{"signature_request_id":"fa5c8a0b0f492d768749333ad6fcc214c111e967","title":"Example agreement"}}`, branch: GetSignatureRequestBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetSignatureRequest(), testConnection, GetSignatureRequestInput{SignatureRequestID: "fa5c8a0b0f492d768749333ad6fcc214c111e967"})
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

func TestGetSignatureRequestRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.hellosign.com/v3", 0)
	for _, signatureRequestID := range []string{"", "../other", "bad\nrequest", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetSignatureRequest(), testConnection, GetSignatureRequestInput{SignatureRequestID: signatureRequestID})
		require.NoError(t, err)
		require.Equal(t, GetSignatureRequestBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"signature_request":{"signature_request_id":"fa5c8a0b0f492d768749333ad6fcc214c111e967","title":"Example agreement","is_complete":true,"is_declined":false,"has_error":false}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetSignatureRequest(), testConnection, GetSignatureRequestInput{SignatureRequestID: "fa5c8a0b0f492d768749333ad6fcc214c111e967"})
	require.NoError(t, err)
	require.Equal(t, GetSignatureRequestBranchInvalidResponse, result.Branch)
}
