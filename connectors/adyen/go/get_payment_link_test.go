// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

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

var testConnection = sdkgo.ConnectionRef{Provider: "adyen", Name: "adyen"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "adyen-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-paymentLink" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-adyen-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetPaymentLinkReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v72/paymentLinks/PL50C5F751CED39G71", request.URL.Path)
		require.Equal(t, "sample-adyen-api-key", request.Header.Get("X-API-Key"))
		_, err := writer.Write([]byte(`{"id":"PL50C5F751CED39G71","status":"completed","amount":{"currency":"EUR"},"shopperEmail":"private@example.test"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v72", 0).GetPaymentLink(), testConnection, GetPaymentLinkInput{PaymentLinkID: "PL50C5F751CED39G71"})
	require.NoError(t, err)
	require.Equal(t, GetPaymentLinkBranchFound, result.Branch)
	require.Equal(t, PaymentLinkStatus{ID: "PL50C5F751CED39G71", Status: "completed", CurrencyCode: "EUR"}, result.Value)
}

func TestGetPaymentLinkClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetPaymentLinkBranchNotFound},
		{name: "forbidden", status: 403, branch: GetPaymentLinkBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"other","status":"completed","amount":{"currency":"EUR"}}`, branch: GetPaymentLinkBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v72", 0).GetPaymentLink(), testConnection, GetPaymentLinkInput{PaymentLinkID: "PL50C5F751CED39G71"})
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

func TestGetPaymentLinkRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://checkout-test.adyen.com/v72", 0)
	for _, paymentLinkID := range []string{"", "../other", "bad\npaymentLink", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetPaymentLink(), testConnection, GetPaymentLinkInput{PaymentLinkID: paymentLinkID})
		require.NoError(t, err)
		require.Equal(t, GetPaymentLinkBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":"PL50C5F751CED39G71","status":"completed","amount":{"currency":"EUR"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v72", 20).GetPaymentLink(), testConnection, GetPaymentLinkInput{PaymentLinkID: "PL50C5F751CED39G71"})
	require.NoError(t, err)
	require.Equal(t, GetPaymentLinkBranchInvalidResponse, result.Branch)
}
