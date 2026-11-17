// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// PriceIDInput identifies one Paddle price.
type PriceIDInput struct {
	// PriceID is a pri_ ID from Catalog > Products > Prices or an API response.
	PriceID string `json:"priceId"`
}

// ListPricesInput selects a page of product prices.
type ListPricesInput struct {
	// PerPage is the page size from 1 through 200.
	PerPage int `json:"perPage"`
	// After is the optional pri_ cursor from the previous page.
	After string `json:"after,omitempty"`
	// ProductID optionally filters to prices for one pro_ product.
	ProductID string `json:"productId,omitempty"`
}

// PriceBillingCycle specifies how often a recurring price is charged.
type PriceBillingCycle struct {
	// Interval is day, week, month, or year.
	Interval string `json:"interval"`
	// Frequency is the positive number of intervals between charges.
	Frequency int `json:"frequency"`
}

// CreatePriceInput specifies a reusable product price.
type CreatePriceInput struct {
	// ProductID is the related pro_ catalog product ID.
	ProductID string `json:"productId"`
	// Description is a private merchant reference of 2–500 characters.
	Description string `json:"description"`
	// Amount is the positive minor-unit amount as an integer string.
	Amount string `json:"amount"`
	// CurrencyCode is a three-letter uppercase ISO 4217 currency code.
	CurrencyCode string `json:"currencyCode"`
	// BillingCycle is omitted for a one-time price.
	BillingCycle *PriceBillingCycle `json:"billingCycle,omitempty"`
}

// UpdatePriceInput replaces a price's description and base amount.
type UpdatePriceInput struct {
	// PriceID identifies the price.
	PriceID string `json:"priceId"`
	// Description is the replacement private merchant reference.
	Description string `json:"description"`
	// Amount is the replacement positive minor-unit amount.
	Amount string `json:"amount"`
	// CurrencyCode is the replacement ISO 4217 currency code.
	CurrencyCode string `json:"currencyCode"`
}

// PaddlePrice contains the primary price configuration.
type PaddlePrice struct {
	// ID is the Paddle price ID.
	ID string `json:"id"`
	// ProductID is the related Paddle product ID.
	ProductID string `json:"productId"`
	// Description is the private merchant reference.
	Description string `json:"description"`
	// Amount is the base price in minor currency units.
	Amount string `json:"amount"`
	// CurrencyCode is the ISO 4217 currency code.
	CurrencyCode string `json:"currencyCode"`
	// Status is active or archived.
	Status string `json:"status"`
	// BillingInterval is blank for one-time prices.
	BillingInterval string `json:"billingInterval"`
	// BillingFrequency is zero for one-time prices.
	BillingFrequency int `json:"billingFrequency"`
}

// PricePage is one page of prices.
type PricePage struct {
	// Items are prices on this page.
	Items []PaddlePrice `json:"items"`
	// HasMore reports whether another page follows.
	HasMore bool `json:"hasMore"`
	// NextCursor is the next page's pri_ ID or blank after the last page.
	NextCursor string `json:"nextCursor"`
}

// ListPricesOperation lists prices.
type ListPricesOperation struct{ client *Client }

// GetPriceOperation reads one price.
type GetPriceOperation struct{ client *Client }

// CreatePriceOperation creates one price.
type CreatePriceOperation struct{ client *Client }

// UpdatePriceOperation replaces a price description and amount.
type UpdatePriceOperation struct{ client *Client }

// ArchivePriceOperation archives a price.
type ArchivePriceOperation struct{ client *Client }

// ListPrices binds the listing Query.
func (client *Client) ListPrices() ListPricesOperation { return ListPricesOperation{client} }

// GetPrice binds the lookup Query.
func (client *Client) GetPrice() GetPriceOperation { return GetPriceOperation{client} }

// CreatePrice binds the creation Mutation.
func (client *Client) CreatePrice() CreatePriceOperation { return CreatePriceOperation{client} }

// UpdatePrice binds the update Mutation.
func (client *Client) UpdatePrice() UpdatePriceOperation { return UpdatePriceOperation{client} }

// ArchivePrice binds the archive Mutation.
func (client *Client) ArchivePrice() ArchivePriceOperation { return ArchivePriceOperation{client} }

// Definition returns the generated listing contract.
func (ListPricesOperation) Definition() sdkgo.QueryDefinition { return ListPricesDefinition }

// Definition returns the generated lookup contract.
func (GetPriceOperation) Definition() sdkgo.QueryDefinition { return GetPriceDefinition }

// Definition returns the generated creation contract.
func (CreatePriceOperation) Definition() sdkgo.MutationDefinition { return CreatePriceDefinition }

// Definition returns the generated update contract.
func (UpdatePriceOperation) Definition() sdkgo.MutationDefinition { return UpdatePriceDefinition }

// Definition returns the generated archive contract.
func (ArchivePriceOperation) Definition() sdkgo.MutationDefinition { return ArchivePriceDefinition }

// IdempotencyKey records call identity; Paddle does not deduplicate price creation on this key.
func (CreatePriceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePriceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records update call identity.
func (UpdatePriceOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePriceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records archive call identity.
func (ArchivePriceOperation) IdempotencyKey(callID sdkgo.CallID, _ PriceIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func validPriceAmount(amount string) bool {
	if len(amount) < 1 || len(amount) > 16 || amount[0] == '0' {
		return false
	}
	for _, character := range amount {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validCurrencyCode(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, character := range code {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func validPriceDescription(description string) bool {
	return len(description) >= 2 && len(description) <= 500 && strings.TrimSpace(description) != ""
}

func validPriceBillingCycle(cycle *PriceBillingCycle) bool {
	if cycle == nil {
		return true
	}
	if cycle.Frequency < 1 {
		return false
	}
	switch cycle.Interval {
	case "day", "week", "month", "year":
		return true
	default:
		return false
	}
}

type priceWire struct {
	ID          string `json:"id"`
	ProductID   string `json:"product_id"`
	Description string `json:"description"`
	UnitPrice   struct {
		Amount       string `json:"amount"`
		CurrencyCode string `json:"currency_code"`
	} `json:"unit_price"`
	BillingCycle *PriceBillingCycle `json:"billing_cycle"`
	Status       string             `json:"status"`
}

func (wire priceWire) project(expectedID string) (PaddlePrice, bool) {
	price := PaddlePrice{ID: wire.ID, ProductID: wire.ProductID, Description: wire.Description, Amount: wire.UnitPrice.Amount, CurrencyCode: wire.UnitPrice.CurrencyCode, Status: wire.Status}
	if wire.BillingCycle != nil {
		price.BillingInterval = wire.BillingCycle.Interval
		price.BillingFrequency = wire.BillingCycle.Frequency
	}
	return price, validPrice(price, expectedID)
}

func validPrice(price PaddlePrice, expectedID string) bool {
	if !validPaddleID(price.ID, "pri_") || expectedID != "" && price.ID != expectedID || !validPaddleID(price.ProductID, "pro_") || !validPriceDescription(price.Description) || !validPriceAmount(price.Amount) || !validCurrencyCode(price.CurrencyCode) || price.Status != "active" && price.Status != "archived" {
		return false
	}
	if price.BillingInterval == "" {
		return price.BillingFrequency == 0
	}
	return validPriceBillingCycle(&PriceBillingCycle{Interval: price.BillingInterval, Frequency: price.BillingFrequency})
}

func decodePaddlePrice(body []byte, expectedID string) (PaddlePrice, bool) {
	var envelope struct {
		Data priceWire `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return PaddlePrice{}, false
	}
	return envelope.Data.project(expectedID)
}

func decodePricePage(body []byte) (PricePage, bool) {
	var envelope struct {
		Data []priceWire `json:"data"`
		Meta struct {
			Pagination struct {
				Next    string `json:"next"`
				HasMore bool   `json:"has_more"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data == nil {
		return PricePage{}, false
	}
	page := PricePage{Items: make([]PaddlePrice, 0, len(envelope.Data)), HasMore: envelope.Meta.Pagination.HasMore}
	for _, item := range envelope.Data {
		price, ok := item.project("")
		if !ok {
			return PricePage{}, false
		}
		page.Items = append(page.Items, price)
	}
	if page.HasMore {
		next, err := url.Parse(envelope.Meta.Pagination.Next)
		if err != nil || next.Path != "/prices" || !validPaddleID(next.Query().Get("after"), "pri_") {
			return PricePage{}, false
		}
		page.NextCursor = next.Query().Get("after")
	}
	return page, true
}

func priceFailure(name string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "paddle", Operation: name, Message: message}
}

// Invoke reads one price page.
func (operation ListPricesOperation) Invoke(call sdkgo.Call, input ListPricesInput) sdkgo.QueryAttempt[PricePage] {
	const name = "listPrices"
	if input.PerPage < 1 || input.PerPage > 200 || input.After != "" && !validPaddleID(input.After, "pri_") || input.ProductID != "" && !validPaddleID(input.ProductID, "pro_") {
		return sdkgo.NewQueryBranch(ListPricesBranchDefect, PricePage{}, priceFailure(name, sdkgo.FailureValidation, "price page input is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	if input.ProductID != "" {
		query.Set("product_id", input.ProductID)
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/prices?"+query.Encode(), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(ListPricesBranchDefect, PricePage{}, priceFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[PricePage](*priceFailure(name, sdkgo.FailureAvailability, "price listing is unavailable"), exchange.retryAfter)
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListPricesBranchProviderRejected, PricePage{}, priceFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected price listing"), sdkgo.Receipt{})
	}
	page, ok := decodePricePage(exchange.body)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(ListPricesBranchInvalidResponse, PricePage{}, priceFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid price page"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListPricesBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one price.
func (operation GetPriceOperation) Invoke(call sdkgo.Call, input PriceIDInput) sdkgo.QueryAttempt[PaddlePrice] {
	const name = "getPrice"
	if !validPaddleID(input.PriceID, "pri_") {
		return sdkgo.NewQueryBranch(GetPriceBranchDefect, PaddlePrice{}, priceFailure(name, sdkgo.FailureValidation, "price ID is invalid"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/prices/"+url.PathEscape(input.PriceID), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(GetPriceBranchDefect, PaddlePrice{}, priceFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[PaddlePrice](*priceFailure(name, sdkgo.FailureAvailability, "price lookup is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetPriceBranchNotFound, PaddlePrice{}, priceFailure(name, sdkgo.FailureNotFound, "price was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(GetPriceBranchProviderRejected, PaddlePrice{}, priceFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected price lookup"), sdkgo.Receipt{})
	}
	price, ok := decodePaddlePrice(exchange.body, input.PriceID)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(GetPriceBranchInvalidResponse, PaddlePrice{}, priceFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid price"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetPriceBranchFound, price, nil, sdkgo.Receipt{})
}

// Invoke creates a price with a bounded number of possible sends.
func (operation CreatePriceOperation) Invoke(call sdkgo.Call, input CreatePriceInput) sdkgo.MutationAttempt[PaddlePrice] {
	const name = "createPrice"
	if !validPaddleID(input.ProductID, "pro_") || !validPriceDescription(input.Description) || !validPriceAmount(input.Amount) || !validCurrencyCode(input.CurrencyCode) || !validPriceBillingCycle(input.BillingCycle) {
		return sdkgo.NewMutationBranch(CreatePriceBranchDefect, PaddlePrice{}, priceFailure(name, sdkgo.FailureValidation, "price product, description, amount, currency, or cycle is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreatePriceDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreatePriceBranchDefect, PaddlePrice{}, priceFailure(name, sdkgo.FailureLocalDefect, "price dispatch fence is unavailable"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Marker.ProviderID != "" {
		readBack := operation.client.exchange(call, call.Context, http.MethodGet, "/prices/"+url.PathEscape(decision.Marker.ProviderID), nil)
		if readBack.status == http.StatusOK && readBack.err == nil {
			price, ok := decodePaddlePrice(readBack.body, decision.Marker.ProviderID)
			if ok && price.ProductID == input.ProductID && price.Amount == input.Amount {
				return sdkgo.NewMutationBranch(CreatePriceBranchCreated, price, nil, sdkgo.Receipt{ProviderObjectID: price.ID})
			}
		}
		if paddleRetryable(readBack) {
			return sdkgo.NewMutationRetry[PaddlePrice](*priceFailure(name, sdkgo.FailureAvailability, "price read-back is unavailable"), readBack.retryAfter)
		}
		return sdkgo.NewMutationUncertain(PaddlePrice{}, *priceFailure(name, sdkgo.FailureProtocol, "created price could not be confirmed"), sdkgo.Receipt{})
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired || decision.MaximumPriorSends >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(PaddlePrice{}, *priceFailure(name, sdkgo.FailureTransport, "price creation outcome is unknown"), sdkgo.Receipt{})
	}
	payload := map[string]any{"product_id": input.ProductID, "description": input.Description, "unit_price": map[string]string{"amount": input.Amount, "currency_code": input.CurrencyCode}}
	if input.BillingCycle != nil {
		payload["billing_cycle"] = input.BillingCycle
	}
	var exchange paddleExchange
	err = fence.Dispatch(func(ctx context.Context) error {
		exchange = operation.client.exchange(call, ctx, http.MethodPost, "/prices", payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[PaddlePrice](*priceFailure(name, sdkgo.FailureTransport, "price dispatch did not start"), 0)
	}
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(CreatePriceBranchDefect, PaddlePrice{}, priceFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if exchange.status == http.StatusTooManyRequests || exchange.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(PaddlePrice{}, *priceFailure(name, sdkgo.FailureTransport, "price retry checkpoint failed"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PaddlePrice](*priceFailure(name, sdkgo.FailureAvailability, "Paddle requested a retry"), exchange.retryAfter)
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddlePrice](*priceFailure(name, sdkgo.FailureAvailability, "price creation outcome is unknown"), exchange.retryAfter)
	}
	if exchange.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PaddlePrice{}, *priceFailure(name, sdkgo.FailureTransport, "an earlier price create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CreatePriceBranchProviderRejected, PaddlePrice{}, priceFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected price creation"), sdkgo.Receipt{})
	}
	price, ok := decodePaddlePrice(exchange.body, "")
	if !ok || exchange.err != nil || price.ProductID != input.ProductID || price.Amount != input.Amount {
		return sdkgo.NewMutationUncertain(PaddlePrice{}, *priceFailure(name, sdkgo.FailureProtocol, "Paddle accepted price creation but returned invalid data"), sdkgo.Receipt{})
	}
	_ = fence.RecordProviderID(price.ID)
	return sdkgo.NewMutationBranch(CreatePriceBranchCreated, price, nil, sdkgo.Receipt{ProviderObjectID: price.ID})
}

func (client *Client) replacePrice(call sdkgo.Call, name, priceID string, payload any, expectedStatus string, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[PaddlePrice] {
	exchange := client.exchange(call, call.Context, http.MethodPatch, "/prices/"+url.PathEscape(priceID), payload)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(defectBranch, PaddlePrice{}, priceFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddlePrice](*priceFailure(name, sdkgo.FailureAvailability, "price update is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(notFoundBranch, PaddlePrice{}, priceFailure(name, sdkgo.FailureNotFound, "price was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewMutationBranch(rejectedBranch, PaddlePrice{}, priceFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected price update"), sdkgo.Receipt{})
	}
	price, ok := decodePaddlePrice(exchange.body, priceID)
	if !ok || exchange.err != nil || expectedStatus != "" && price.Status != expectedStatus {
		return sdkgo.NewMutationBranch(invalidBranch, PaddlePrice{}, priceFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid price"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(successBranch, price, nil, sdkgo.Receipt{ProviderObjectID: price.ID})
}

// Invoke replaces a price's description and base amount.
func (operation UpdatePriceOperation) Invoke(call sdkgo.Call, input UpdatePriceInput) sdkgo.MutationAttempt[PaddlePrice] {
	const name = "updatePrice"
	if !validPaddleID(input.PriceID, "pri_") || !validPriceDescription(input.Description) || !validPriceAmount(input.Amount) || !validCurrencyCode(input.CurrencyCode) {
		return sdkgo.NewMutationBranch(UpdatePriceBranchDefect, PaddlePrice{}, priceFailure(name, sdkgo.FailureValidation, "price ID, description, amount, or currency is invalid"), sdkgo.Receipt{})
	}
	payload := map[string]any{"description": input.Description, "unit_price": map[string]string{"amount": input.Amount, "currency_code": input.CurrencyCode}}
	return operation.client.replacePrice(call, name, input.PriceID, payload, "", UpdatePriceBranchUpdated, UpdatePriceBranchNotFound, UpdatePriceBranchProviderRejected, UpdatePriceBranchInvalidResponse, UpdatePriceBranchDefect)
}

// Invoke archives a price with an absolute status replacement.
func (operation ArchivePriceOperation) Invoke(call sdkgo.Call, input PriceIDInput) sdkgo.MutationAttempt[PaddlePrice] {
	const name = "archivePrice"
	if !validPaddleID(input.PriceID, "pri_") {
		return sdkgo.NewMutationBranch(ArchivePriceBranchDefect, PaddlePrice{}, priceFailure(name, sdkgo.FailureValidation, "price ID is invalid"), sdkgo.Receipt{})
	}
	return operation.client.replacePrice(call, name, input.PriceID, map[string]string{"status": "archived"}, "archived", ArchivePriceBranchArchived, ArchivePriceBranchNotFound, ArchivePriceBranchProviderRejected, ArchivePriceBranchInvalidResponse, ArchivePriceBranchDefect)
}
