// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFinancialMutationsRetryUnknownOutcomes(t *testing.T) {
	operations := []struct {
		name     string
		rejected sdkgo.BranchID
		run      func(*Client, dex.Context) (sdkgo.BranchID, error)
	}{
		{"chargePaymentMethod", ChargePaymentMethodBranchProviderRejected, func(client *Client, ctx dex.Context) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(ctx, client.ChargePaymentMethod(), testConnection, ChargePaymentMethodInput{PaymentMethodID: "payment-1", Amount: "10.00"})
			return result.Branch, err
		}},
		{"captureTransaction", CaptureTransactionBranchProviderRejected, func(client *Client, ctx dex.Context) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(ctx, client.CaptureTransaction(), testConnection, CaptureTransactionInput{TransactionID: "transaction-1"})
			return result.Branch, err
		}},
		{"voidTransaction", VoidTransactionBranchProviderRejected, func(client *Client, ctx dex.Context) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(ctx, client.VoidTransaction(), testConnection, GetTransactionInput{TransactionID: "transaction-1"})
			return result.Branch, err
		}},
		{"refundTransaction", RefundTransactionBranchProviderRejected, func(client *Client, ctx dex.Context) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(ctx, client.RefundTransaction(), testConnection, RefundTransactionInput{TransactionID: "transaction-1"})
			return result.Branch, err
		}},
	}
	responses := []struct {
		name         string
		status       int
		body         string
		priorAttempt bool
		retry        bool
		maxBytes     int64
	}{
		{"conflict", 409, `{"error":"conflict"}`, false, true, 0},
		{"method not allowed", 405, `{"error":"method"}`, false, true, 0},
		{"empty bad request", 400, ``, false, true, 0},
		{"first definite refusal", 400, `{"error":"invalid"}`, false, false, 0},
		{"prior definite refusal", 403, `{"error":"forbidden"}`, true, true, 0},
		{"errors and data", 200, `{"data":{"chargePaymentMethod":{"transaction":{"id":"transaction-1","status":"SETTLED"}}},"errors":[{"message":"invalid","extensions":{"errorClass":"VALIDATION"}}]}`, false, true, 0},
		{"unparseable errors", 200, `{"errors":[{"message":"invalid"}]}`, false, true, 0},
		{"incomplete success", 200, `{"data":{}}`, false, true, 0},
		{"oversized success", 200, `{"data":{"chargePaymentMethod":{"transaction":{"id":"transaction-1","status":"SETTLED"}}}}`, false, true, 16},
	}
	for _, operation := range operations {
		for _, response := range responses {
			t.Run(operation.name+"/"+response.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.WriteHeader(response.status)
					_, err := io.WriteString(writer, response.body)
					require.NoError(t, err)
				}))
				defer server.Close()
				ctx := singledispatchtest.NewContext("financial-test", operation.name)
				if response.priorAttempt {
					ctx.AdvanceAttempt()
				}
				branch, err := operation.run(newTestClient(t, server.URL, response.maxBytes), ctx)
				if response.retry {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
					return
				}
				require.NoError(t, err)
				require.Equal(t, operation.rejected, branch)
			})
		}
	}
}
