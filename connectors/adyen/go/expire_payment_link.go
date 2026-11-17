// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ExpirePaymentLinkInput selects one link whose status should become expired.
type ExpirePaymentLinkInput struct {
	// PaymentLinkID is the non-secret Adyen link ID.
	PaymentLinkID string `json:"paymentLinkId"`
}

// ExpirePaymentLinkOperation forces the selected link to expire.
type ExpirePaymentLinkOperation struct{ client *Client }

// ExpirePaymentLink binds the Mutation to this client.
func (client *Client) ExpirePaymentLink() ExpirePaymentLinkOperation {
	return ExpirePaymentLinkOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (ExpirePaymentLinkOperation) Definition() sdkgo.MutationDefinition {
	return ExpirePaymentLinkDefinition
}

// IdempotencyKey derives a stable call identity; the provider sets an absolute status.
func (ExpirePaymentLinkOperation) IdempotencyKey(callID sdkgo.CallID, _ ExpirePaymentLinkInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends the same expiry setting on every attempt.
func (operation ExpirePaymentLinkOperation) Invoke(call sdkgo.Call, input ExpirePaymentLinkInput) sdkgo.MutationAttempt[PaymentLinkStatus] {
	if !validPaymentLinkID(input.PaymentLinkID) {
		return expirePaymentLinkBranch(ExpirePaymentLinkBranchDefect, sdkgo.FailureValidation, "payment link ID must contain 1–128 URL-safe characters")
	}
	response := operation.client.patch(call, "/paymentLinks/"+url.PathEscape(input.PaymentLinkID), []byte(`{"status":"expired"}`))
	if errors.Is(response.err, errCredential) {
		return expirePaymentLinkBranch(ExpirePaymentLinkBranchDefect, sdkgo.FailureAuthentication, "Adyen credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[PaymentLinkStatus](expirePaymentLinkFailure(sdkgo.FailureTransport, "Adyen request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[PaymentLinkStatus](expirePaymentLinkFailure(sdkgo.FailureRateLimit, "Adyen rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[PaymentLinkStatus](expirePaymentLinkFailure(sdkgo.FailureAvailability, "Adyen is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return expirePaymentLinkBranch(ExpirePaymentLinkBranchNotFound, sdkgo.FailureNotFound, "Adyen did not expose the payment link")
	}
	if response.status != http.StatusOK {
		return expirePaymentLinkBranch(ExpirePaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Adyen rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return expirePaymentLinkBranch(ExpirePaymentLinkBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Adyen response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[PaymentLinkStatus](expirePaymentLinkFailure(sdkgo.FailureTransport, "Adyen response was interrupted"), 0)
	}
	status, err := decodePaymentLink(response.body, input.PaymentLinkID)
	if err != nil || status.Status != "expired" {
		return expirePaymentLinkBranch(ExpirePaymentLinkBranchInvalidResponse, sdkgo.FailureProtocol, "Adyen returned invalid expired payment link JSON")
	}
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adyen", ProviderObjectID: input.PaymentLinkID, ObservedAt: time.Now().UTC()}
	return sdkgo.NewMutationBranch(ExpirePaymentLinkBranchExpired, status, nil, receipt)
}

func expirePaymentLinkFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adyen", Operation: "expirePaymentLink", Message: message}
}

func expirePaymentLinkBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PaymentLinkStatus] {
	failure := expirePaymentLinkFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, PaymentLinkStatus{}, &failure, sdkgo.Receipt{})
}
