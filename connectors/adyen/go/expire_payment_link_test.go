// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestExpirePaymentLinkSendsAbsoluteStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPatch, request.Method)
		require.Equal(t, "/v72/paymentLinks/PL50C5F751CED39G71", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		require.Equal(t, "sample-adyen-api-key", request.Header.Get("X-API-Key"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"status":"expired"}`, string(body))
		_, err = writer.Write([]byte(`{"id":"PL50C5F751CED39G71","status":"expired","amount":{"currency":"EUR"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/v72", 0).ExpirePaymentLink(), testConnection, ExpirePaymentLinkInput{PaymentLinkID: "PL50C5F751CED39G71"})
	require.NoError(t, err)
	require.Equal(t, ExpirePaymentLinkBranchExpired, result.Branch)
	require.Equal(t, PaymentLinkStatus{ID: "PL50C5F751CED39G71", Status: "expired", CurrencyCode: "EUR"}, result.Value)
}

func TestExpirePaymentLinkClassifiesOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: ExpirePaymentLinkBranchNotFound},
		{name: "forbidden", status: 403, branch: ExpirePaymentLinkBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "server failure", status: 500, retry: true},
		{name: "malformed", status: 200, body: `{"id":"PL50C5F751CED39G71","status":"active","amount":{"currency":"EUR"}}`, branch: ExpirePaymentLinkBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/v72", 0).ExpirePaymentLink(), testConnection, ExpirePaymentLinkInput{PaymentLinkID: "PL50C5F751CED39G71"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, "https://checkout-test.adyen.com/v72", 0).ExpirePaymentLink(), testConnection, ExpirePaymentLinkInput{})
	require.NoError(t, err)
	require.Equal(t, ExpirePaymentLinkBranchDefect, result.Branch)
}
