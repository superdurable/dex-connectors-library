// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package ses

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "amazon-ses", Name: "amazon-ses"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "ses-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-email-identity" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKIASESCONNECTOR"), SecretAccessKey: sdkgo.NewSecretString("ses-test-secret"), SessionToken: sdkgo.NewSecretString("")}
	client, err := New(Config{Region: "eu-west-1", Endpoint: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	return client
}
func TestGetEmailIdentitySignsAndReadsConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v2/email/identities/example.com", r.URL.Path)
		require.Equal(t, "20261030T000000Z", r.Header.Get("X-Amz-Date"))
		require.True(t, strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/ses/aws4_request"))
		_, err := w.Write([]byte(`{"VerificationStatus":"SUCCESS","IdentityType":"DOMAIN"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetEmailIdentity(), testConnection, GetEmailIdentityInput{EmailIdentity: "example.com"})
	require.NoError(t, err)
	require.Equal(t, GetEmailIdentityBranchFound, result.Branch)
	require.Equal(t, EmailIdentityDetails{Name: "example.com", VerificationStatus: "SUCCESS"}, result.Value)
}
func TestGetEmailIdentityClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{"message":"not found"}`, GetEmailIdentityBranchMissing, false},
		{"forbidden", 403, `{"message":"denied"}`, GetEmailIdentityBranchProviderRejected, false},
		{"rate limit", 429, `{"message":"busy"}`, "", true},
		{"unavailable", 503, `{"message":"unavailable"}`, "", true},
		{"bad response", 200, `{"VerificationStatus":"UNKNOWN"}`, GetEmailIdentityBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetEmailIdentity(), testConnection, GetEmailIdentityInput{EmailIdentity: "example.com"})
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
func TestGetEmailIdentityRejectsInvalidNameAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://email.eu-west-1.amazonaws.com", 0)
	for _, name := range []string{"", "bad/name"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetEmailIdentity(), testConnection, GetEmailIdentityInput{EmailIdentity: name})
		require.NoError(t, err)
		require.Equal(t, GetEmailIdentityBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"VerificationStatus":"SUCCESS"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 10).GetEmailIdentity(), testConnection, GetEmailIdentityInput{EmailIdentity: "example.com"})
	require.NoError(t, err)
	require.Equal(t, GetEmailIdentityBranchInvalidResponse, result.Branch)
}
