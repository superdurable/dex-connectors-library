// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var sampleListPaymentMethodsInput = ListPaymentMethodsInput{
	MerchantAccount: "TestMerchant", CurrencyCode: "EUR", AmountMinorUnits: 1200, CountryCode: "NL",
}

func TestListPaymentMethodsFiltersStoredDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v72/paymentMethods", request.URL.Path)
		require.Empty(t, request.Header.Get("Idempotency-Key"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var requestFields map[string]any
		require.NoError(t, json.Unmarshal(body, &requestFields))
		require.Equal(t, "TestMerchant", requestFields["merchantAccount"])
		require.Equal(t, "NL", requestFields["countryCode"])
		require.Equal(t, map[string]any{"currency": "EUR", "value": float64(1200)}, requestFields["amount"])
		_, err = writer.Write([]byte(`{"paymentMethods":[{"type":"scheme","name":"Cards","configuration":{"private":"secret"}},{"type":"ideal","name":"iDEAL"}],"storedPaymentMethods":[{"bankAccountNumber":"private"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v72", 0).ListPaymentMethods(), testConnection, sampleListPaymentMethodsInput)
	require.NoError(t, err)
	require.Equal(t, ListPaymentMethodsBranchListed, result.Branch)
	require.Equal(t, PaymentMethodPage{Methods: []AvailablePaymentMethod{{Type: "scheme", Name: "Cards"}, {Type: "ideal", Name: "iDEAL"}}}, result.Value)
}

func TestListPaymentMethodsClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "rejected", status: 400, branch: ListPaymentMethodsBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "unknown outcome", status: 500, retry: true},
		{name: "invalid", status: 200, body: `{"paymentMethods":[{"name":"Missing code"}]}`, branch: ListPaymentMethodsBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v72", 0).ListPaymentMethods(), testConnection, sampleListPaymentMethodsInput)
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
	invalidInput := sampleListPaymentMethodsInput
	invalidInput.CurrencyCode = "eur"
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "https://checkout-test.adyen.com/v72", 0).ListPaymentMethods(), testConnection, invalidInput)
	require.NoError(t, err)
	require.Equal(t, ListPaymentMethodsBranchDefect, result.Branch)
}
