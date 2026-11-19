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

const modificationResponse = `{"merchantAccount":"TestMerchant","paymentPspReference":"881617891407032C","pspReference":"991617891407032C","status":"received","shopperEmail":"private@example.test"}`

func TestPaymentModificationsUseProviderIdempotencyAndExcludeShopperData(t *testing.T) {
	baseInput := PaymentModificationInput{MerchantAccount: "TestMerchant", PaymentPspReference: "881617891407032C", Reference: "case-100"}
	for _, testCase := range []struct {
		name, path string
		hasAmount  bool
		run        func(*Client, PaymentModificationInput) (sdkgo.BranchID, PaymentModificationReceipt, error)
	}{
		{"capture", "captures", true, func(client *Client, input PaymentModificationInput) (sdkgo.BranchID, PaymentModificationReceipt, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.CapturePayment(), testConnection, input)
			return result.Branch, result.Value, err
		}},
		{"cancel", "cancels", false, func(client *Client, input PaymentModificationInput) (sdkgo.BranchID, PaymentModificationReceipt, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.CancelPayment(), testConnection, PaymentReferenceModificationInput{MerchantAccount: input.MerchantAccount, PaymentPspReference: input.PaymentPspReference, Reference: input.Reference})
			return result.Branch, result.Value, err
		}},
		{"refund", "refunds", true, func(client *Client, input PaymentModificationInput) (sdkgo.BranchID, PaymentModificationReceipt, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.RefundPayment(), testConnection, input)
			return result.Branch, result.Value, err
		}},
		{"reverse", "reversals", false, func(client *Client, input PaymentModificationInput) (sdkgo.BranchID, PaymentModificationReceipt, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.ReversePayment(), testConnection, PaymentReferenceModificationInput{MerchantAccount: input.MerchantAccount, PaymentPspReference: input.PaymentPspReference, Reference: input.Reference})
			return result.Branch, result.Value, err
		}},
		{"amount", "amountUpdates", true, func(client *Client, input PaymentModificationInput) (sdkgo.BranchID, PaymentModificationReceipt, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.UpdateAuthorisedAmount(), testConnection, input)
			return result.Branch, result.Value, err
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input := baseInput
			if testCase.hasAmount {
				input.CurrencyCode = "EUR"
				input.AmountMinorUnits = 1200
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "/v72/payments/881617891407032C/"+testCase.path, r.URL.Path)
				require.Equal(t, "sample-adyen-api-key", r.Header.Get("X-API-Key"))
				require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				var request map[string]any
				require.NoError(t, json.Unmarshal(body, &request))
				require.Equal(t, "TestMerchant", request["merchantAccount"])
				if testCase.hasAmount {
					require.Equal(t, map[string]any{"currency": "EUR", "value": float64(1200)}, request["amount"])
				} else {
					require.NotContains(t, request, "amount")
				}
				w.WriteHeader(http.StatusCreated)
				_, err = w.Write([]byte(modificationResponse))
				require.NoError(t, err)
			}))
			defer server.Close()
			branch, value, err := testCase.run(newTestClient(t, server.URL+"/v72", 0), input)
			require.NoError(t, err)
			require.Equal(t, sdkgo.BranchID("received"), branch)
			require.Equal(t, PaymentModificationReceipt{PaymentPspReference: "881617891407032C", ModificationPspReference: "991617891407032C", Status: "received"}, value)
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private@example.test")
		})
	}
}

func TestPaymentModificationClassifiesRetryAndRejection(t *testing.T) {
	input := PaymentModificationInput{MerchantAccount: "TestMerchant", PaymentPspReference: "881617891407032C"}
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"rejected", 400, `{}`, CancelPaymentBranchProviderRejected, false},
		{"rateLimit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"unknown", 500, `{}`, "", true},
		{"invalid", 201, `{}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, err := w.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/v72", 0).CancelPayment(), testConnection, PaymentReferenceModificationInput{MerchantAccount: input.MerchantAccount, PaymentPspReference: input.PaymentPspReference})
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
