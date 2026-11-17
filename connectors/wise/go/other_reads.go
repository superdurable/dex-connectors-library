// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var quoteUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ListRecipientsInput selects a profile and a bounded result size.
type ListRecipientsInput struct {
	// ProfileID is the positive decimal Wise profile ID.
	ProfileID string `json:"profileId"`
	// Size is the maximum recipient count, from 1 to 100.
	Size int `json:"size"`
}

// RecipientSummary excludes names and bank account details.
type RecipientSummary struct {
	// ID is the Wise recipient account ID.
	ID string `json:"id"`
	// Currency is the recipient's ISO 4217 currency.
	Currency string `json:"currency"`
	// Active reports whether the account may be selected.
	Active bool `json:"active"`
}

// RecipientsOutput contains sanitized recipient summaries.
type RecipientsOutput struct {
	// Recipients is the bounded recipient list.
	Recipients []RecipientSummary `json:"recipients"`
}

// ListRecipientsOperation reads recipient metadata for one profile.
type ListRecipientsOperation struct{ client *Client }

// ListRecipients binds the Query to this client.
func (client *Client) ListRecipients() ListRecipientsOperation {
	return ListRecipientsOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListRecipientsOperation) Definition() sdkgo.QueryDefinition { return ListRecipientsDefinition }

// Invoke lists recipient IDs, currencies, and active flags.
func (operation ListRecipientsOperation) Invoke(call sdkgo.Call, input ListRecipientsInput) sdkgo.QueryAttempt[RecipientsOutput] {
	if !validTransferID(input.ProfileID) || input.Size < 1 || input.Size > 100 {
		return wiseReadBranch[RecipientsOutput]("listRecipients", sdkgo.DefectBranchID, sdkgo.FailureValidation, "profile ID or size is invalid")
	}
	query := url.Values{"profileId": {input.ProfileID}, "size": {strconv.Itoa(input.Size)}}
	return readWiseJSON(call, operation.client, "listRecipients", "/accounts?"+query.Encode(), ListRecipientsBranchListed, false, decodeRecipients)
}

// GetExchangeRatesInput selects one ISO 4217 currency pair.
type GetExchangeRatesInput struct {
	// Source is the three-letter source currency code.
	Source string `json:"source"`
	// Target is the three-letter target currency code.
	Target string `json:"target"`
}

// ExchangeRate is a Wise rate at a documented timestamp.
type ExchangeRate struct {
	// Source is the source ISO 4217 currency.
	Source string `json:"source"`
	// Target is the target ISO 4217 currency.
	Target string `json:"target"`
	// Rate is the quoted multiplier.
	Rate float64 `json:"rate"`
	// Time is Wise's rate timestamp.
	Time string `json:"time"`
}

// ExchangeRatesOutput contains one or more rates for the pair.
type ExchangeRatesOutput struct {
	// Rates is the provider rate list.
	Rates []ExchangeRate `json:"rates"`
}

// GetExchangeRatesOperation reads Wise exchange rates.
type GetExchangeRatesOperation struct{ client *Client }

// GetExchangeRates binds the Query to this client.
func (client *Client) GetExchangeRates() GetExchangeRatesOperation {
	return GetExchangeRatesOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetExchangeRatesOperation) Definition() sdkgo.QueryDefinition {
	return GetExchangeRatesDefinition
}

// Invoke gets Wise's current rates for one currency pair.
func (operation GetExchangeRatesOperation) Invoke(call sdkgo.Call, input GetExchangeRatesInput) sdkgo.QueryAttempt[ExchangeRatesOutput] {
	if !validCurrency(input.Source) || !validCurrency(input.Target) || input.Source == input.Target {
		return wiseReadBranch[ExchangeRatesOutput]("getExchangeRates", sdkgo.DefectBranchID, sdkgo.FailureValidation, "source and target must be distinct ISO 4217 currencies")
	}
	query := url.Values{"source": {input.Source}, "target": {input.Target}}
	return readWiseJSON(call, operation.client, "getExchangeRates", "/rates?"+query.Encode(), GetExchangeRatesBranchListed, false, decodeExchangeRates)
}

// GetQuoteInput selects an authenticated profile quote.
type GetQuoteInput struct {
	// ProfileID is the positive decimal Wise profile ID.
	ProfileID string `json:"profileId"`
	// QuoteID is the Wise quote UUID.
	QuoteID string `json:"quoteId"`
}

// QuoteSummary excludes recipient and payment details.
type QuoteSummary struct {
	// ID is the quote UUID.
	ID string `json:"id"`
	// SourceCurrency is the source ISO 4217 currency.
	SourceCurrency string `json:"sourceCurrency"`
	// TargetCurrency is the target ISO 4217 currency.
	TargetCurrency string `json:"targetCurrency"`
}

// GetQuoteOperation reads an authenticated quote.
type GetQuoteOperation struct{ client *Client }

// GetQuote binds the Query to this client.
func (client *Client) GetQuote() GetQuoteOperation { return GetQuoteOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetQuoteOperation) Definition() sdkgo.QueryDefinition { return GetQuoteDefinition }

// Invoke gets a quote without retaining recipient or payment details.
func (operation GetQuoteOperation) Invoke(call sdkgo.Call, input GetQuoteInput) sdkgo.QueryAttempt[QuoteSummary] {
	if !validTransferID(input.ProfileID) || !quoteUUID.MatchString(input.QuoteID) {
		return wiseReadBranch[QuoteSummary]("getQuote", sdkgo.DefectBranchID, sdkgo.FailureValidation, "profile ID or quote UUID is invalid")
	}
	path := "/profiles/" + url.PathEscape(input.ProfileID) + "/quotes/" + url.PathEscape(input.QuoteID)
	return readWiseJSON(call, operation.client, "getQuote", path, GetQuoteBranchFound, true, decodeQuote)
}

func validCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func decodeRecipients(body []byte) (RecipientsOutput, error) {
	var wire []struct {
		ID       int64  `json:"id"`
		Currency string `json:"currency"`
		Active   bool   `json:"active"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return RecipientsOutput{}, err
	}
	if wire == nil {
		return RecipientsOutput{}, fmt.Errorf("recipient list missing")
	}
	output := RecipientsOutput{Recipients: make([]RecipientSummary, 0, len(wire))}
	for _, recipient := range wire {
		if recipient.ID <= 0 || !validCurrency(recipient.Currency) {
			return RecipientsOutput{}, fmt.Errorf("recipient ID or currency missing")
		}
		output.Recipients = append(output.Recipients, RecipientSummary{ID: strconv.FormatInt(recipient.ID, 10), Currency: recipient.Currency, Active: recipient.Active})
	}
	return output, nil
}

func decodeExchangeRates(body []byte) (ExchangeRatesOutput, error) {
	var rates []ExchangeRate
	if err := json.Unmarshal(body, &rates); err != nil {
		return ExchangeRatesOutput{}, err
	}
	if rates == nil {
		return ExchangeRatesOutput{}, fmt.Errorf("rate list missing")
	}
	for _, rate := range rates {
		if !validCurrency(rate.Source) || !validCurrency(rate.Target) || rate.Rate <= 0 || strings.TrimSpace(rate.Time) == "" {
			return ExchangeRatesOutput{}, fmt.Errorf("rate fields missing")
		}
	}
	return ExchangeRatesOutput{Rates: rates}, nil
}

func decodeQuote(body []byte) (QuoteSummary, error) {
	var quote QuoteSummary
	if err := json.Unmarshal(body, &quote); err != nil {
		return QuoteSummary{}, err
	}
	if !quoteUUID.MatchString(quote.ID) || !validCurrency(quote.SourceCurrency) || !validCurrency(quote.TargetCurrency) {
		return QuoteSummary{}, fmt.Errorf("quote ID or currencies missing")
	}
	return quote, nil
}
