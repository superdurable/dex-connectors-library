// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetPaymentLinkInput selects a payment link by its non-secret Adyen ID.
type GetPaymentLinkInput struct {
	// PaymentLinkID is a payment link ID returned by Adyen Checkout API.
	PaymentLinkID string `json:"paymentLinkId"`
}

// PaymentLinkStatus excludes shopper identities and payment details.
type PaymentLinkStatus struct {
	// ID is the Adyen payment link ID.
	ID string `json:"id"`
	// Status is a payment link state such as completed.
	Status string `json:"status"`
	// CurrencyCode is the link amount's ISO 4217 currency.
	CurrencyCode string `json:"currencyCode"`
}

// GetPaymentLinkOperation reads one payment link's identity and state.
type GetPaymentLinkOperation struct{ client *Client }

// GetPaymentLink binds the Query to this client.
func (client *Client) GetPaymentLink() GetPaymentLinkOperation {
	return GetPaymentLinkOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetPaymentLinkOperation) Definition() sdkgo.QueryDefinition { return GetPaymentLinkDefinition }

// Invoke reads one payment link and classifies retryable and definitive outcomes.
func (operation GetPaymentLinkOperation) Invoke(call sdkgo.Call, input GetPaymentLinkInput) sdkgo.QueryAttempt[PaymentLinkStatus] {
	if !validPaymentLinkID(input.PaymentLinkID) {
		return paymentLinkBranch(GetPaymentLinkBranchDefect, sdkgo.FailureValidation, "payment link ID must contain 1–128 URL-safe characters")
	}
	response := operation.client.get(call, "/paymentLinks/"+url.PathEscape(input.PaymentLinkID))
	if errors.Is(response.err, errCredential) {
		return paymentLinkBranch(GetPaymentLinkBranchDefect, sdkgo.FailureAuthentication, "Adyen credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PaymentLinkStatus](paymentLinkFailure(sdkgo.FailureTransport, "Adyen request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PaymentLinkStatus](paymentLinkFailure(sdkgo.FailureRateLimit, "Adyen rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PaymentLinkStatus](paymentLinkFailure(sdkgo.FailureAvailability, "Adyen is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return paymentLinkBranch(GetPaymentLinkBranchNotFound, sdkgo.FailureNotFound, "Adyen did not expose the payment link")
	}
	if response.status != http.StatusOK {
		return paymentLinkBranch(GetPaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Adyen rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return paymentLinkBranch(GetPaymentLinkBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Adyen response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PaymentLinkStatus](paymentLinkFailure(sdkgo.FailureTransport, "Adyen response was interrupted"), 0)
	}
	paymentLinkStatus, err := decodePaymentLink(response.body, input.PaymentLinkID)
	if err != nil {
		return paymentLinkBranch(GetPaymentLinkBranchInvalidResponse, sdkgo.FailureProtocol, "Adyen returned invalid payment link JSON")
	}
	return sdkgo.NewQueryBranch(GetPaymentLinkBranchFound, paymentLinkStatus, nil, sdkgo.Receipt{})
}

func validPaymentLinkID(paymentLinkID string) bool {
	if len(paymentLinkID) == 0 || len(paymentLinkID) > 128 {
		return false
	}
	for _, character := range paymentLinkID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodePaymentLink(body []byte, requestedPaymentLinkID string) (PaymentLinkStatus, error) {
	var wire struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Amount struct {
			Currency string `json:"currency"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return PaymentLinkStatus{}, err
	}
	if wire.ID != requestedPaymentLinkID || strings.TrimSpace(wire.Status) == "" || len(wire.Amount.Currency) != 3 {
		return PaymentLinkStatus{}, fmt.Errorf("payment link identity, status, or currency missing")
	}
	return PaymentLinkStatus{ID: wire.ID, Status: wire.Status, CurrencyCode: wire.Amount.Currency}, nil
}

func paymentLinkFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adyen", Operation: "getPaymentLink", Message: message}
}

func paymentLinkBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PaymentLinkStatus] {
	failure := paymentLinkFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PaymentLinkStatus{}, &failure, sdkgo.Receipt{})
}
