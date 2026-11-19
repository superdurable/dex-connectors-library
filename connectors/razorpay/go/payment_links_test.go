// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const testPaymentLinkID = "plink_ExjpAUN3gVHrPJ"

func TestPaymentLinkQueriesExcludeCustomerDetails(t *testing.T) {
	link := `{"id":"` + testPaymentLinkID + `","status":"created","amount":1000,"currency":"INR","reference_id":"merchant-42","short_url":"https://rzp.io/i/example","customer":{"email":"private@example.com"}}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/payment_links" {
			require.Equal(t, "merchant-42", request.URL.Query().Get("reference_id"))
			_, err := writer.Write([]byte(`{"payment_links":[` + link + `]}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "/api/payment_links/"+testPaymentLinkID, request.URL.Path)
		_, err := writer.Write([]byte(link))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListPaymentLinks(), testConnection, ListPaymentLinksInput{ReferenceID: "merchant-42"})
	require.NoError(t, err)
	require.Equal(t, ListPaymentLinksBranchListed, listed.Branch)
	require.Len(t, listed.Value.Links, 1)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetPaymentLink(), testConnection, GetPaymentLinkInput{PaymentLinkID: testPaymentLinkID})
	require.NoError(t, err)
	require.Equal(t, GetPaymentLinkBranchFound, found.Branch)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.com")
}

func TestCreatePaymentLinkUsesCallReferenceAndDisablesNotifications(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/payment_links", request.URL.Path)
		var body struct {
			Amount      int64  `json:"amount"`
			Currency    string `json:"currency"`
			ReferenceID string `json:"reference_id"`
			Notify      struct {
				SMS   bool `json:"sms"`
				Email bool `json:"email"`
			} `json:"notify"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, int64(1000), body.Amount)
		require.Equal(t, "INR", body.Currency)
		require.Regexp(t, `^dex_[0-9a-f]{32}$`, body.ReferenceID)
		require.False(t, body.Notify.SMS)
		require.False(t, body.Notify.Email)
		_, err := writer.Write([]byte(`{"id":"` + testPaymentLinkID + `","status":"created","amount":1000,"currency":"INR","reference_id":"` + body.ReferenceID + `","short_url":"https://rzp.io/i/example"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("razorpay-link", "create-link"), newTestClient(t, server.URL+"/api", 0).CreatePaymentLink(), testConnection, CreatePaymentLinkInput{Amount: 1000, CurrencyCode: "INR"})
	require.NoError(t, err)
	require.Equal(t, CreatePaymentLinkBranchCreated, result.Branch)
	require.Equal(t, testPaymentLinkID, result.Value.ID)
}

func TestCancelPaymentLinkConfirmsAlreadyCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		require.Equal(t, "/api/payment_links/"+testPaymentLinkID, request.URL.Path)
		_, err := writer.Write([]byte(`{"id":"` + testPaymentLinkID + `","status":"cancelled","amount":1000,"currency":"INR","reference_id":"merchant-42","short_url":"https://rzp.io/i/example"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("razorpay-link", "cancel-link"), newTestClient(t, server.URL+"/api", 0).CancelPaymentLink(), testConnection, CancelPaymentLinkInput{PaymentLinkID: testPaymentLinkID})
	require.NoError(t, err)
	require.Equal(t, CancelPaymentLinkBranchCancelled, result.Branch)
}
