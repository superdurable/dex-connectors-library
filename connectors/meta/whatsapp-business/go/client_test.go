// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package whatsappbusiness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "secret-meta-system-user-token"

var testConnection = sdkgo.ConnectionRef{Provider: "whatsapp-business", Name: "whatsapp-business"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "whatsapp-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes},
		sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)}})
	require.NoError(t, err)
	return client
}

func TestListPhoneNumbersPaginationAndProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/123456789/phone_numbers", r.URL.Path)
		require.Equal(t, "cursor/+value", r.URL.Query().Get("after"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"data":[{"id":"1906385232743451","verified_name":"Example Store","display_phone_number":"+1 631-555-5555","quality_rating":"GREEN"}],"paging":{"cursors":{"after":"next-cursor"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListPhoneNumbers(), testConnection,
		ListPhoneNumbersInput{BusinessAccountID: "123456789", After: "cursor/+value"})
	require.NoError(t, err)
	require.Equal(t, ListPhoneNumbersBranchListed, result.Branch)
	require.Equal(t, "next-cursor", result.Value.NextCursor)
	require.Equal(t, "Example Store", result.Value.Numbers[0].VerifiedName)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "quality_rating")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestListPhoneNumbersRetriesRateLimitAndServerFailure(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListPhoneNumbers(), testConnection,
			ListPhoneNumbersInput{BusinessAccountID: "123456789"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
}

func TestListPhoneNumbersInvalidInputAndProviderResponses(t *testing.T) {
	var calls atomic.Int32
	var body atomic.Value
	body.Store(`{"data":{}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, err := w.Write([]byte(body.Load().(string)))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListPhoneNumbers(), testConnection,
		ListPhoneNumbersInput{BusinessAccountID: "bad/id"})
	require.NoError(t, err)
	require.Equal(t, ListPhoneNumbersBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
	result, err = sdkgo.RunQuery(queryContext{}, client.ListPhoneNumbers(), testConnection,
		ListPhoneNumbersInput{BusinessAccountID: "123456789"})
	require.NoError(t, err)
	require.Equal(t, ListPhoneNumbersBranchInvalidResponse, result.Branch)
	body.Store(`{"data":[{"id":"1906385232743451","verified_name":"Example Store","display_phone_number":"+1 631-555-5555"}]}`)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListPhoneNumbers(), testConnection,
		ListPhoneNumbersInput{BusinessAccountID: "123456789"})
	require.NoError(t, err)
	require.Equal(t, ListPhoneNumbersBranchListed, result.Branch)
	require.EqualValues(t, 2, calls.Load())
}

func TestListPhoneNumbersDefinitiveProviderResponses(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(status.Load()))
		body := `{"error":{"message":"not found"}}`
		if status.Load() == http.StatusForbidden {
			body = `{"error":{"message":"secret-meta-system-user-token"}}`
		}
		_, err := w.Write([]byte(body))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	for _, testCase := range []struct {
		status int
		branch sdkgo.BranchID
	}{
		{http.StatusNotFound, ListPhoneNumbersBranchNotFound},
		{http.StatusForbidden, ListPhoneNumbersBranchDefect},
	} {
		status.Store(int32(testCase.status))
		result, err := sdkgo.RunQuery(queryContext{}, client.ListPhoneNumbers(), testConnection,
			ListPhoneNumbersInput{BusinessAccountID: "123456789"})
		require.NoError(t, err)
		require.Equal(t, testCase.branch, result.Branch)
		require.NotContains(t, result.Failure.Message, testAccessToken)
	}
}
