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

func TestLookupCardBrandsByBINSendsOnlySixDigits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v72/cardDetails", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		require.Empty(t, r.Header.Get("Idempotency-Key"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"merchantAccount":"TestMerchant","cardNumber":"411111","countryCode":"US"}`, string(body))
		_, err = w.Write([]byte(`{"brands":[{"type":"visa","supported":true,"fundingSource":"CREDIT"}],"issuerCountry":"US"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v72", 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.LookupCardBrandsByBIN(), testConnection, LookupCardBrandsByBINInput{MerchantAccount: "TestMerchant", IssuerBIN: "411111", CountryCode: "US"})
	require.NoError(t, err)
	require.Equal(t, LookupCardBrandsByBINBranchFound, result.Branch)
	require.Equal(t, CardBrands{Brands: []CardBrand{{Type: "visa", Supported: true}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "fundingSource")
}

func TestLookupCardBrandsByBINRejectsFullCardNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v72", 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.LookupCardBrandsByBIN(), testConnection, LookupCardBrandsByBINInput{MerchantAccount: "TestMerchant", IssuerBIN: "4111111111111111"})
	require.NoError(t, err)
	require.Equal(t, LookupCardBrandsByBINBranchDefect, result.Branch)
}
