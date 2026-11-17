// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListPaymentMethodsInput describes a checkout context without shopper identity.
type ListPaymentMethodsInput struct {
	// MerchantAccount is the account shown in Adyen Customer Area > Settings > Merchant accounts.
	MerchantAccount string `json:"merchantAccount"`
	// CurrencyCode is the three-letter ISO currency of the proposed payment.
	CurrencyCode string `json:"currencyCode"`
	// AmountMinorUnits is the positive proposed amount in currency minor units.
	AmountMinorUnits int64 `json:"amountMinorUnits"`
	// CountryCode optionally narrows methods to an uppercase two-letter shopper country.
	CountryCode string `json:"countryCode,omitempty"`
}

// AvailablePaymentMethod has only the provider code and display name.
type AvailablePaymentMethod struct {
	// Type is the payment method code used by Adyen.
	Type string `json:"type"`
	// Name is its shopper-facing display name.
	Name string `json:"name"`
}

// PaymentMethodPage excludes stored shopper methods and payment data.
type PaymentMethodPage struct {
	// Methods are available for this merchant, amount, currency, and country.
	Methods []AvailablePaymentMethod `json:"methods"`
}

// ListPaymentMethodsOperation reads available Checkout methods.
type ListPaymentMethodsOperation struct{ client *Client }

// ListPaymentMethods binds the Query to this client.
func (client *Client) ListPaymentMethods() ListPaymentMethodsOperation {
	return ListPaymentMethodsOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListPaymentMethodsOperation) Definition() sdkgo.QueryDefinition {
	return ListPaymentMethodsDefinition
}

// Invoke reads method names and codes without persisting stored payment details.
func (operation ListPaymentMethodsOperation) Invoke(call sdkgo.Call, input ListPaymentMethodsInput) sdkgo.QueryAttempt[PaymentMethodPage] {
	if !validPaymentMethodsInput(input) {
		return paymentMethodsBranch(ListPaymentMethodsBranchDefect, sdkgo.FailureValidation, "payment method request fields are invalid")
	}
	requestBody, err := json.Marshal(struct {
		MerchantAccount string `json:"merchantAccount"`
		Amount          struct {
			Currency string `json:"currency"`
			Value    int64  `json:"value"`
		} `json:"amount"`
		CountryCode string `json:"countryCode,omitempty"`
	}{MerchantAccount: input.MerchantAccount, Amount: struct {
		Currency string `json:"currency"`
		Value    int64  `json:"value"`
	}{Currency: input.CurrencyCode, Value: input.AmountMinorUnits}, CountryCode: input.CountryCode})
	if err != nil {
		return paymentMethodsBranch(ListPaymentMethodsBranchDefect, sdkgo.FailureLocalDefect, "payment methods request could not be encoded")
	}
	response := operation.client.queryPost(call, "/paymentMethods", requestBody)
	if errors.Is(response.err, errCredential) {
		return paymentMethodsBranch(ListPaymentMethodsBranchDefect, sdkgo.FailureAuthentication, "Adyen credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PaymentMethodPage](paymentMethodsFailure(sdkgo.FailureTransport, "Adyen request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PaymentMethodPage](paymentMethodsFailure(sdkgo.FailureRateLimit, "Adyen rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PaymentMethodPage](paymentMethodsFailure(sdkgo.FailureAvailability, "Adyen is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return paymentMethodsBranch(ListPaymentMethodsBranchProviderRejected, sdkgo.FailureProviderRejection, "Adyen rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return paymentMethodsBranch(ListPaymentMethodsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Adyen response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PaymentMethodPage](paymentMethodsFailure(sdkgo.FailureTransport, "Adyen response was interrupted"), 0)
	}
	page, err := decodePaymentMethodPage(response.body)
	if err != nil {
		return paymentMethodsBranch(ListPaymentMethodsBranchInvalidResponse, sdkgo.FailureProtocol, "Adyen returned invalid payment methods JSON")
	}
	return sdkgo.NewQueryBranch(ListPaymentMethodsBranchListed, page, nil, sdkgo.Receipt{})
}

func validPaymentMethodsInput(input ListPaymentMethodsInput) bool {
	if input.MerchantAccount == "" || len(input.MerchantAccount) > 128 || strings.TrimSpace(input.MerchantAccount) != input.MerchantAccount || strings.ContainsAny(input.MerchantAccount, "\r\n") || input.AmountMinorUnits <= 0 || len(input.CurrencyCode) != 3 {
		return false
	}
	for _, character := range input.CurrencyCode {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	if input.CountryCode != "" {
		if len(input.CountryCode) != 2 {
			return false
		}
		for _, character := range input.CountryCode {
			if character < 'A' || character > 'Z' {
				return false
			}
		}
	}
	return true
}

func decodePaymentMethodPage(body []byte) (PaymentMethodPage, error) {
	var wire struct {
		PaymentMethods []AvailablePaymentMethod `json:"paymentMethods"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return PaymentMethodPage{}, err
	}
	if wire.PaymentMethods == nil {
		return PaymentMethodPage{}, errors.New("paymentMethods missing")
	}
	if len(wire.PaymentMethods) > 500 {
		return PaymentMethodPage{}, errors.New("too many payment methods")
	}
	for _, method := range wire.PaymentMethods {
		if method.Type == "" || method.Name == "" {
			return PaymentMethodPage{}, errors.New("method code or name missing")
		}
	}
	return PaymentMethodPage{Methods: wire.PaymentMethods}, nil
}

func paymentMethodsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adyen", Operation: "listPaymentMethods", Message: message}
}

func paymentMethodsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PaymentMethodPage] {
	failure := paymentMethodsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PaymentMethodPage{}, &failure, sdkgo.Receipt{})
}
