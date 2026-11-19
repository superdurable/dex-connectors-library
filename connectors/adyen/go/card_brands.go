// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var sixDigitBIN = regexp.MustCompile(`^[0-9]{6}$`)

// LookupCardBrandsByBINInput supplies only a six-digit issuer prefix, never a full card number.
type LookupCardBrandsByBINInput struct {
	// MerchantAccount is the account shown in Customer Area > Settings > Merchant accounts.
	MerchantAccount string `json:"merchantAccount"`
	// IssuerBIN is exactly the first six card digits; never supply the remaining digits.
	IssuerBIN string `json:"issuerBin"`
	// CountryCode optionally narrows supported brands to an uppercase two-letter shopper country.
	CountryCode string `json:"countryCode,omitempty"`
}

// CardBrand identifies one returned scheme and merchant support decision.
type CardBrand struct {
	// Type is the provider's scheme code.
	Type string `json:"type"`
	// Supported reports whether this merchant can accept the brand.
	Supported bool `json:"supported"`
}

// CardBrands contains only sanitized brand and support information.
type CardBrands struct {
	// Brands is the returned brand list.
	Brands []CardBrand `json:"brands"`
}

// LookupCardBrandsByBINOperation reads supported brands for a six-digit prefix.
type LookupCardBrandsByBINOperation struct{ client *Client }

// LookupCardBrandsByBIN binds the read-only Query.
func (client *Client) LookupCardBrandsByBIN() LookupCardBrandsByBINOperation {
	return LookupCardBrandsByBINOperation{client}
}

// Definition returns the generated Query contract.
func (LookupCardBrandsByBINOperation) Definition() sdkgo.QueryDefinition {
	return LookupCardBrandsByBINDefinition
}

// Invoke sends only a six-digit prefix to Adyen and omits all card metadata beyond brand support.
func (operation LookupCardBrandsByBINOperation) Invoke(call sdkgo.Call, input LookupCardBrandsByBINInput) sdkgo.QueryAttempt[CardBrands] {
	if input.MerchantAccount == "" || len(input.MerchantAccount) > 128 || strings.TrimSpace(input.MerchantAccount) != input.MerchantAccount || strings.ContainsAny(input.MerchantAccount, "\r\n") || !sixDigitBIN.MatchString(input.IssuerBIN) || input.CountryCode != "" && !validCountryCode(input.CountryCode) {
		return cardBrandsBranch(LookupCardBrandsByBINBranchDefect, sdkgo.FailureValidation, "merchant account, BIN, or country code is invalid")
	}
	requestBody, err := json.Marshal(struct {
		MerchantAccount string `json:"merchantAccount"`
		CardNumber      string `json:"cardNumber"`
		CountryCode     string `json:"countryCode,omitempty"`
	}{input.MerchantAccount, input.IssuerBIN, input.CountryCode})
	if err != nil {
		return cardBrandsBranch(LookupCardBrandsByBINBranchDefect, sdkgo.FailureLocalDefect, "card brand request could not be encoded")
	}
	response := operation.client.queryPost(call, "/cardDetails", requestBody)
	if errors.Is(response.err, errCredential) {
		return cardBrandsBranch(LookupCardBrandsByBINBranchDefect, sdkgo.FailureAuthentication, "Adyen credentials unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CardBrands](cardBrandsFailure(sdkgo.FailureTransport, "Adyen request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CardBrands](cardBrandsFailure(sdkgo.FailureRateLimit, "Adyen rate limited the lookup"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CardBrands](cardBrandsFailure(sdkgo.FailureAvailability, "Adyen unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return cardBrandsBranch(LookupCardBrandsByBINBranchProviderRejected, sdkgo.FailureProviderRejection, "Adyen rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return cardBrandsBranch(LookupCardBrandsByBINBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Adyen response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CardBrands](cardBrandsFailure(sdkgo.FailureTransport, "Adyen response interrupted"), 0)
	}
	var wire struct {
		Brands []CardBrand `json:"brands"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Brands == nil {
		return cardBrandsBranch(LookupCardBrandsByBINBranchInvalidResponse, sdkgo.FailureProtocol, "Adyen returned invalid brand JSON")
	}
	for _, brand := range wire.Brands {
		if brand.Type == "" || len(brand.Type) > 64 || strings.ContainsAny(brand.Type, "\r\n") {
			return cardBrandsBranch(LookupCardBrandsByBINBranchInvalidResponse, sdkgo.FailureProtocol, "Adyen returned invalid brand code")
		}
	}
	return sdkgo.NewQueryBranch(LookupCardBrandsByBINBranchFound, CardBrands{Brands: wire.Brands}, nil, sdkgo.Receipt{})
}

func validCountryCode(value string) bool {
	if len(value) != 2 {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func cardBrandsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adyen", Operation: "lookupCardBrandsByBIN", Message: message}
}

func cardBrandsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CardBrands] {
	failure := cardBrandsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CardBrands{}, &failure, sdkgo.Receipt{})
}
