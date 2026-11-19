// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CreatePaymentLinkInput supplies the required Checkout fields and an optional shopper country.
type CreatePaymentLinkInput struct {
	// MerchantAccount is the account shown in Adyen Customer Area > Settings > Merchant accounts.
	MerchantAccount string `json:"merchantAccount"`
	// Reference identifies the payment in future Adyen status communications.
	Reference string `json:"reference"`
	// CurrencyCode is the three-letter ISO currency of the amount.
	CurrencyCode string `json:"currencyCode"`
	// AmountMinorUnits is the positive amount in the currency's minor units.
	AmountMinorUnits int64 `json:"amountMinorUnits"`
	// CountryCode optionally narrows available payment methods to a two-letter shopper country.
	CountryCode string `json:"countryCode,omitempty"`
}

// CreatedPaymentLink contains the shopper-facing URL and non-secret payment link identity.
type CreatedPaymentLink struct {
	// ID identifies the link for later reads and expiry.
	ID string `json:"id"`
	// Status is the link status at creation.
	Status string `json:"status"`
	// CurrencyCode is the confirmed ISO currency.
	CurrencyCode string `json:"currencyCode"`
	// AmountMinorUnits is the confirmed amount in minor units.
	AmountMinorUnits int64 `json:"amountMinorUnits"`
	// URL is the shopper-facing payment page; share it only with the intended shopper.
	URL string `json:"url"`
}

// CreatePaymentLinkOperation creates one Checkout payment link.
type CreatePaymentLinkOperation struct{ client *Client }

// CreatePaymentLink binds the Mutation to this client.
func (client *Client) CreatePaymentLink() CreatePaymentLinkOperation {
	return CreatePaymentLinkOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreatePaymentLinkOperation) Definition() sdkgo.MutationDefinition {
	return CreatePaymentLinkDefinition
}

// IdempotencyKey supplies Adyen a stable key across Dex retries.
func (CreatePaymentLinkOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePaymentLinkInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the link once per Adyen idempotency key, retrying unknown outcomes safely.
func (operation CreatePaymentLinkOperation) Invoke(call sdkgo.Call, input CreatePaymentLinkInput) sdkgo.MutationAttempt[CreatedPaymentLink] {
	if !validCreatePaymentLinkInput(input) || len(call.IdempotencyKey) == 0 || len(call.IdempotencyKey) > 64 {
		return createPaymentLinkBranch(CreatePaymentLinkBranchDefect, sdkgo.FailureValidation, "payment link fields or idempotency key are invalid")
	}
	requestBody, err := json.Marshal(struct {
		Amount struct {
			Currency string `json:"currency"`
			Value    int64  `json:"value"`
		} `json:"amount"`
		MerchantAccount string `json:"merchantAccount"`
		Reference       string `json:"reference"`
		CountryCode     string `json:"countryCode,omitempty"`
	}{Amount: struct {
		Currency string `json:"currency"`
		Value    int64  `json:"value"`
	}{Currency: input.CurrencyCode, Value: input.AmountMinorUnits}, MerchantAccount: input.MerchantAccount, Reference: input.Reference, CountryCode: input.CountryCode})
	if err != nil {
		return createPaymentLinkBranch(CreatePaymentLinkBranchDefect, sdkgo.FailureLocalDefect, "payment link request could not be encoded")
	}
	response := operation.client.post(call, "/paymentLinks", requestBody)
	if errors.Is(response.err, errCredential) {
		return createPaymentLinkBranch(CreatePaymentLinkBranchDefect, sdkgo.FailureAuthentication, "Adyen credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[CreatedPaymentLink](createPaymentLinkFailure(sdkgo.FailureTransport, "Adyen request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CreatedPaymentLink](createPaymentLinkFailure(sdkgo.FailureRateLimit, "Adyen rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CreatedPaymentLink](createPaymentLinkFailure(sdkgo.FailureAvailability, "Adyen is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusCreated {
		return createPaymentLinkBranch(CreatePaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Adyen rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return createPaymentLinkBranch(CreatePaymentLinkBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Adyen response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[CreatedPaymentLink](createPaymentLinkFailure(sdkgo.FailureTransport, "Adyen response was interrupted"), 0)
	}
	created, err := decodeCreatedPaymentLink(response.body, input)
	if err != nil {
		return createPaymentLinkBranch(CreatePaymentLinkBranchInvalidResponse, sdkgo.FailureProtocol, "Adyen returned invalid payment link JSON")
	}
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adyen", ProviderObjectID: created.ID, ObservedAt: time.Now().UTC()}
	return sdkgo.NewMutationBranch(CreatePaymentLinkBranchCreated, created, nil, receipt)
}

func validCreatePaymentLinkInput(input CreatePaymentLinkInput) bool {
	if strings.TrimSpace(input.MerchantAccount) != input.MerchantAccount || input.MerchantAccount == "" || len(input.MerchantAccount) > 128 || strings.ContainsAny(input.MerchantAccount, "\r\n") {
		return false
	}
	if strings.TrimSpace(input.Reference) != input.Reference || input.Reference == "" || len(input.Reference) > 80 || strings.ContainsAny(input.Reference, "\r\n") {
		return false
	}
	if len(input.CurrencyCode) != 3 || input.AmountMinorUnits <= 0 {
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

func decodeCreatedPaymentLink(body []byte, input CreatePaymentLinkInput) (CreatedPaymentLink, error) {
	var wire struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		URL    string `json:"url"`
		Amount struct {
			Currency string `json:"currency"`
			Value    int64  `json:"value"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CreatedPaymentLink{}, err
	}
	parsedURL, err := url.Parse(wire.URL)
	if err != nil {
		return CreatedPaymentLink{}, err
	}
	if !validPaymentLinkID(wire.ID) || wire.Status != "active" || wire.Amount.Currency != input.CurrencyCode || wire.Amount.Value != input.AmountMinorUnits || parsedURL.Scheme != "https" || parsedURL.Host == "" || parsedURL.User != nil || parsedURL.Fragment != "" {
		return CreatedPaymentLink{}, errors.New("payment link identity, amount, status, or URL invalid")
	}
	return CreatedPaymentLink{ID: wire.ID, Status: wire.Status, CurrencyCode: wire.Amount.Currency, AmountMinorUnits: wire.Amount.Value, URL: wire.URL}, nil
}

func createPaymentLinkFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adyen", Operation: "createPaymentLink", Message: message}
}

func createPaymentLinkBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedPaymentLink] {
	failure := createPaymentLinkFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, CreatedPaymentLink{}, &failure, sdkgo.Receipt{})
}
