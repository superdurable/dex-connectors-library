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

var sampleCreatePaymentLinkInput = CreatePaymentLinkInput{
	MerchantAccount: "TestMerchant", Reference: "order-100", CurrencyCode: "EUR", AmountMinorUnits: 1200, CountryCode: "NL",
}

const sampleCreatedPaymentLinkJSON = `{"id":"PL50C5F751CED39G71","status":"active","amount":{"currency":"EUR","value":1200},"url":"https://test.adyen.link/PL50C5F751CED39G71","shopperEmail":"private@example.test"}`

func TestCreatePaymentLinkSendsIdempotencyKeyAndRequiredFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v72/paymentLinks", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		require.NotEmpty(t, request.Header.Get("Idempotency-Key"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(body, &fields))
		require.Equal(t, "TestMerchant", fields["merchantAccount"])
		require.Equal(t, "order-100", fields["reference"])
		require.Equal(t, "NL", fields["countryCode"])
		require.Equal(t, map[string]any{"currency": "EUR", "value": float64(1200)}, fields["amount"])
		writer.WriteHeader(http.StatusCreated)
		_, err = writer.Write([]byte(sampleCreatedPaymentLinkJSON))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/v72", 0).CreatePaymentLink(), testConnection, sampleCreatePaymentLinkInput)
	require.NoError(t, err)
	require.Equal(t, CreatePaymentLinkBranchCreated, result.Branch)
	require.Equal(t, CreatedPaymentLink{ID: "PL50C5F751CED39G71", Status: "active", CurrencyCode: "EUR", AmountMinorUnits: 1200, URL: "https://test.adyen.link/PL50C5F751CED39G71"}, result.Value)
}

func TestCreatePaymentLinkClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "rejected", status: 400, branch: CreatePaymentLinkBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "unknown outcome", status: 500, retry: true},
		{name: "invalid URL", status: 201, body: `{"id":"PL50C5F751CED39G71","status":"active","amount":{"currency":"EUR","value":1200},"url":"http://example.test/link"}`, branch: CreatePaymentLinkBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/v72", 0).CreatePaymentLink(), testConnection, sampleCreatePaymentLinkInput)
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
	invalidInput := sampleCreatePaymentLinkInput
	invalidInput.AmountMinorUnits = 0
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, "https://checkout-test.adyen.com/v72", 0).CreatePaymentLink(), testConnection, invalidInput)
	require.NoError(t, err)
	require.Equal(t, CreatePaymentLinkBranchDefect, result.Branch)
}
