// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetPaymentInput selects a payment by its non-secret Razorpay ID.
type GetPaymentInput struct {
	// PaymentID is a Razorpay payment ID prefixed pay_.
	PaymentID string `json:"paymentId"`
}

// PaymentStatus excludes customer identities and payment details.
type PaymentStatus struct {
	// ID is the Razorpay payment ID.
	ID string `json:"id"`
	// Status is a Razorpay payment state such as captured.
	Status string `json:"status"`
	// Amount is the positive payment amount in currency subunits.
	Amount int64 `json:"amount"`
	// CurrencyCode is the payment's ISO 4217 currency.
	CurrencyCode string `json:"currencyCode"`
}

// GetPaymentOperation reads one payment's identity and state.
type GetPaymentOperation struct{ client *Client }

// GetPayment binds the Query to this client.
func (client *Client) GetPayment() GetPaymentOperation {
	return GetPaymentOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetPaymentOperation) Definition() sdkgo.QueryDefinition { return GetPaymentDefinition }

// Invoke reads one payment and classifies retryable and definitive outcomes.
func (operation GetPaymentOperation) Invoke(call sdkgo.Call, input GetPaymentInput) sdkgo.QueryAttempt[PaymentStatus] {
	if !validPaymentID(input.PaymentID) {
		return paymentBranch(GetPaymentBranchDefect, sdkgo.FailureValidation, "payment ID must start with pay_ and contain 6–64 letters or digits")
	}
	response := operation.client.get(call, "/payments/"+url.PathEscape(input.PaymentID))
	if errors.Is(response.err, errCredential) {
		return paymentBranch(GetPaymentBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PaymentStatus](paymentFailure(sdkgo.FailureTransport, "Razorpay request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PaymentStatus](paymentFailure(sdkgo.FailureRateLimit, "Razorpay rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PaymentStatus](paymentFailure(sdkgo.FailureAvailability, "Razorpay is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound || (response.status == http.StatusBadRequest && response.err == nil && isRazorpayPaymentMissing(response.body)) {
		return paymentBranch(GetPaymentBranchNotFound, sdkgo.FailureNotFound, "Razorpay did not expose the payment")
	}
	if response.status != http.StatusOK {
		return paymentBranch(GetPaymentBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return paymentBranch(GetPaymentBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Razorpay response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PaymentStatus](paymentFailure(sdkgo.FailureTransport, "Razorpay response was interrupted"), 0)
	}
	paymentStatus, err := decodePayment(response.body, input.PaymentID)
	if err != nil {
		return paymentBranch(GetPaymentBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay returned invalid payment JSON")
	}
	return sdkgo.NewQueryBranch(GetPaymentBranchFound, paymentStatus, nil, sdkgo.Receipt{})
}

var paymentIDPattern = regexp.MustCompile(`^pay_[A-Za-z0-9]{6,64}$`)

func validPaymentID(paymentID string) bool { return paymentIDPattern.MatchString(paymentID) }

// isRazorpayPaymentMissing reports the documented HTTP 400 error Razorpay
// returns for an unknown payment ID or one from another account or mode.
func isRazorpayPaymentMissing(body []byte) bool {
	var wire struct {
		Error struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return false
	}
	return wire.Error.Code == "BAD_REQUEST_ERROR" && wire.Error.Description == "The id provided does not exist"
}

func decodePayment(body []byte, requestedPaymentID string) (PaymentStatus, error) {
	var wire struct {
		ID       string `json:"id"`
		Entity   string `json:"entity"`
		Status   string `json:"status"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return PaymentStatus{}, err
	}
	if wire.ID != requestedPaymentID || wire.Entity != "payment" || strings.TrimSpace(wire.Status) == "" || wire.Amount <= 0 || len(wire.Currency) != 3 {
		return PaymentStatus{}, fmt.Errorf("payment identity, status, amount, or currency missing")
	}
	return PaymentStatus{ID: wire.ID, Status: wire.Status, Amount: wire.Amount, CurrencyCode: wire.Currency}, nil
}

func paymentFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "razorpay", Operation: "getPayment", Message: message}
}

func paymentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PaymentStatus] {
	failure := paymentFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PaymentStatus{}, &failure, sdkgo.Receipt{})
}
