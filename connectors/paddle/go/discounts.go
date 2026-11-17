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

// DiscountIDInput identifies one Paddle discount.
type DiscountIDInput struct {
	// DiscountID is a dsc_ ID from Catalog > Discounts or an API response.
	DiscountID string `json:"discountId"`
}

// ListDiscountsInput selects one page of discounts.
type ListDiscountsInput struct {
	// PerPage is the page size from 1 through 200.
	PerPage int `json:"perPage"`
	// After is the optional dsc_ cursor from the previous page.
	After string `json:"after,omitempty"`
	// Code optionally filters for one exact checkout code.
	Code string `json:"code,omitempty"`
}

// CreateDiscountInput specifies a percentage checkout discount.
type CreateDiscountInput struct {
	// Code is a unique 1–32 character alphanumeric checkout code.
	Code string `json:"code"`
	// Description is a private reference label from 1–500 characters.
	Description string `json:"description"`
	// Amount is a percentage from 0.01 through 100, represented as a decimal string.
	Amount string `json:"amount"`
}

// UpdateDiscountInput replaces a discount's description and percentage amount.
type UpdateDiscountInput struct {
	// DiscountID identifies the discount.
	DiscountID string `json:"discountId"`
	// Description is the replacement private reference label.
	Description string `json:"description"`
	// Amount is the replacement percentage as a decimal string.
	Amount string `json:"amount"`
}

// PaddleDiscount contains a discount's identity and public configuration.
type PaddleDiscount struct {
	// ID is the Paddle discount ID.
	ID string `json:"id"`
	// Code is the customer checkout code.
	Code string `json:"code"`
	// Description is the private merchant reference label.
	Description string `json:"description"`
	// Type is the discount calculation type.
	Type string `json:"type"`
	// Amount is the percentage or minor-unit amount as a decimal string.
	Amount string `json:"amount"`
	// Status is active or archived.
	Status string `json:"status"`
}

// DiscountPage is one page of discounts.
type DiscountPage struct {
	// Items are discounts on this page.
	Items []PaddleDiscount `json:"items"`
	// HasMore reports whether another page follows.
	HasMore bool `json:"hasMore"`
	// NextCursor is the next page's dsc_ ID or blank after the last page.
	NextCursor string `json:"nextCursor"`
}

// ListDiscountsOperation lists discounts.
type ListDiscountsOperation struct{ client *Client }

// GetDiscountOperation reads one discount.
type GetDiscountOperation struct{ client *Client }

// CreateDiscountOperation creates a percentage discount.
type CreateDiscountOperation struct{ client *Client }

// UpdateDiscountOperation replaces a discount's amount and description.
type UpdateDiscountOperation struct{ client *Client }

// ArchiveDiscountOperation archives a discount.
type ArchiveDiscountOperation struct{ client *Client }

// ListDiscounts binds the listing Query.
func (client *Client) ListDiscounts() ListDiscountsOperation { return ListDiscountsOperation{client} }

// GetDiscount binds the lookup Query.
func (client *Client) GetDiscount() GetDiscountOperation { return GetDiscountOperation{client} }

// CreateDiscount binds the creation Mutation.
func (client *Client) CreateDiscount() CreateDiscountOperation {
	return CreateDiscountOperation{client}
}

// UpdateDiscount binds the update Mutation.
func (client *Client) UpdateDiscount() UpdateDiscountOperation {
	return UpdateDiscountOperation{client}
}

// ArchiveDiscount binds the archive Mutation.
func (client *Client) ArchiveDiscount() ArchiveDiscountOperation {
	return ArchiveDiscountOperation{client}
}

// Definition returns the generated listing contract.
func (ListDiscountsOperation) Definition() sdkgo.QueryDefinition { return ListDiscountsDefinition }

// Definition returns the generated lookup contract.
func (GetDiscountOperation) Definition() sdkgo.QueryDefinition { return GetDiscountDefinition }

// Definition returns the generated creation contract.
func (CreateDiscountOperation) Definition() sdkgo.MutationDefinition { return CreateDiscountDefinition }

// Definition returns the generated update contract.
func (UpdateDiscountOperation) Definition() sdkgo.MutationDefinition { return UpdateDiscountDefinition }

// Definition returns the generated archive contract.
func (ArchiveDiscountOperation) Definition() sdkgo.MutationDefinition {
	return ArchiveDiscountDefinition
}

// IdempotencyKey records call identity; Paddle does not deduplicate creates on this key.
func (CreateDiscountOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDiscountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records update call identity.
func (UpdateDiscountOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateDiscountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records archive call identity.
func (ArchiveDiscountOperation) IdempotencyKey(callID sdkgo.CallID, _ DiscountIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func validDiscountCode(code string) bool {
	if len(code) < 1 || len(code) > 32 {
		return false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			if character < 'A' || character > 'Z' {
				if character < 'a' || character > 'z' {
					return false
				}
			}
		}
	}
	return true
}

func validPercentageAmount(amount string) bool {
	value, err := strconv.ParseFloat(amount, 64)
	return err == nil && value >= 0.01 && value <= 100 && len(amount) <= 8
}

func validDiscountDescription(description string) bool {
	return strings.TrimSpace(description) != "" && len(description) <= 500
}

func decodePaddleDiscount(body []byte, expectedID string) (PaddleDiscount, bool) {
	var envelope struct {
		Data PaddleDiscount `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || !validDiscount(envelope.Data, expectedID) {
		return PaddleDiscount{}, false
	}
	return envelope.Data, true
}

func validDiscount(discount PaddleDiscount, expectedID string) bool {
	return validPaddleID(discount.ID, "dsc_") && (expectedID == "" || discount.ID == expectedID) && discount.Description != "" && discount.Amount != "" && discount.Type != "" && (discount.Status == "active" || discount.Status == "archived")
}

func decodeDiscountPage(body []byte) (DiscountPage, bool) {
	var envelope struct {
		Data []PaddleDiscount `json:"data"`
		Meta struct {
			Pagination struct {
				Next    string `json:"next"`
				HasMore bool   `json:"has_more"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data == nil {
		return DiscountPage{}, false
	}
	page := DiscountPage{Items: envelope.Data, HasMore: envelope.Meta.Pagination.HasMore}
	for _, discount := range page.Items {
		if !validDiscount(discount, "") {
			return DiscountPage{}, false
		}
	}
	if page.HasMore {
		next, err := url.Parse(envelope.Meta.Pagination.Next)
		if err != nil || next.Path != "/discounts" || !validPaddleID(next.Query().Get("after"), "dsc_") {
			return DiscountPage{}, false
		}
		page.NextCursor = next.Query().Get("after")
	}
	return page, true
}

func discountFailure(name string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "paddle", Operation: name, Message: message}
}

// Invoke reads one discount page.
func (operation ListDiscountsOperation) Invoke(call sdkgo.Call, input ListDiscountsInput) sdkgo.QueryAttempt[DiscountPage] {
	const name = "listDiscounts"
	if input.PerPage < 1 || input.PerPage > 200 || input.After != "" && !validPaddleID(input.After, "dsc_") || input.Code != "" && !validDiscountCode(input.Code) {
		return sdkgo.NewQueryBranch(ListDiscountsBranchDefect, DiscountPage{}, discountFailure(name, sdkgo.FailureValidation, "discount page input is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	if input.Code != "" {
		query.Set("code", input.Code)
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/discounts?"+query.Encode(), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(ListDiscountsBranchDefect, DiscountPage{}, discountFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[DiscountPage](*discountFailure(name, sdkgo.FailureAvailability, "discount listing is unavailable"), exchange.retryAfter)
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListDiscountsBranchProviderRejected, DiscountPage{}, discountFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected discount listing"), sdkgo.Receipt{})
	}
	page, ok := decodeDiscountPage(exchange.body)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(ListDiscountsBranchInvalidResponse, DiscountPage{}, discountFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid discount page"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListDiscountsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one discount.
func (operation GetDiscountOperation) Invoke(call sdkgo.Call, input DiscountIDInput) sdkgo.QueryAttempt[PaddleDiscount] {
	const name = "getDiscount"
	if !validPaddleID(input.DiscountID, "dsc_") {
		return sdkgo.NewQueryBranch(GetDiscountBranchDefect, PaddleDiscount{}, discountFailure(name, sdkgo.FailureValidation, "discount ID is invalid"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/discounts/"+url.PathEscape(input.DiscountID), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(GetDiscountBranchDefect, PaddleDiscount{}, discountFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[PaddleDiscount](*discountFailure(name, sdkgo.FailureAvailability, "discount lookup is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetDiscountBranchNotFound, PaddleDiscount{}, discountFailure(name, sdkgo.FailureNotFound, "discount was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(GetDiscountBranchProviderRejected, PaddleDiscount{}, discountFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected discount lookup"), sdkgo.Receipt{})
	}
	discount, ok := decodePaddleDiscount(exchange.body, input.DiscountID)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(GetDiscountBranchInvalidResponse, PaddleDiscount{}, discountFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid discount"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetDiscountBranchFound, discount, nil, sdkgo.Receipt{})
}

// Invoke creates a percentage discount with bounded possible sends and code read-back.
func (operation CreateDiscountOperation) Invoke(call sdkgo.Call, input CreateDiscountInput) sdkgo.MutationAttempt[PaddleDiscount] {
	const name = "createDiscount"
	if !validDiscountCode(input.Code) || !validDiscountDescription(input.Description) || !validPercentageAmount(input.Amount) {
		return sdkgo.NewMutationBranch(CreateDiscountBranchDefect, PaddleDiscount{}, discountFailure(name, sdkgo.FailureValidation, "discount code, description, or percentage is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateDiscountDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateDiscountBranchDefect, PaddleDiscount{}, discountFailure(name, sdkgo.FailureLocalDefect, "discount dispatch fence is unavailable"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(PaddleDiscount{}, *discountFailure(name, sdkgo.FailureTransport, "discount creation outcome is unknown"), sdkgo.Receipt{})
	}
	if decision.Kind != singledispatch.SendAllowed {
		query := url.Values{"code": {input.Code}, "per_page": {"1"}}
		readBack := operation.client.exchange(call, call.Context, http.MethodGet, "/discounts?"+query.Encode(), nil)
		if readBack.status == http.StatusOK && readBack.err == nil {
			page, ok := decodeDiscountPage(readBack.body)
			if !ok {
				return sdkgo.NewMutationUncertain(PaddleDiscount{}, *discountFailure(name, sdkgo.FailureProtocol, "discount read-back is invalid"), sdkgo.Receipt{})
			}
			if len(page.Items) == 1 && strings.EqualFold(page.Items[0].Code, input.Code) && page.Items[0].Description == input.Description && page.Items[0].Amount == input.Amount {
				return sdkgo.NewMutationBranch(CreateDiscountBranchCreated, page.Items[0], nil, sdkgo.Receipt{ProviderObjectID: page.Items[0].ID})
			}
			if len(page.Items) != 0 {
				return sdkgo.NewMutationUncertain(PaddleDiscount{}, *discountFailure(name, sdkgo.FailureProtocol, "discount read-back disagreed with request"), sdkgo.Receipt{})
			}
		} else if paddleRetryable(readBack) {
			return sdkgo.NewMutationRetry[PaddleDiscount](*discountFailure(name, sdkgo.FailureAvailability, "discount read-back is unavailable"), readBack.retryAfter)
		} else {
			return sdkgo.NewMutationUncertain(PaddleDiscount{}, *discountFailure(name, sdkgo.FailureTransport, "discount read-back was rejected"), sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired || decision.MaximumPriorSends >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(PaddleDiscount{}, *discountFailure(name, sdkgo.FailureTransport, "discount creation reached its send cap"), sdkgo.Receipt{})
		}
	}
	var exchange paddleExchange
	err = fence.Dispatch(func(ctx context.Context) error {
		exchange = operation.client.exchange(call, ctx, http.MethodPost, "/discounts", map[string]string{"code": input.Code, "description": input.Description, "type": "percentage", "amount": input.Amount})
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[PaddleDiscount](*discountFailure(name, sdkgo.FailureTransport, "discount dispatch did not start"), 0)
	}
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(CreateDiscountBranchDefect, PaddleDiscount{}, discountFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if exchange.status == http.StatusTooManyRequests || exchange.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(PaddleDiscount{}, *discountFailure(name, sdkgo.FailureTransport, "discount retry checkpoint failed"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PaddleDiscount](*discountFailure(name, sdkgo.FailureAvailability, "Paddle requested a retry"), exchange.retryAfter)
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddleDiscount](*discountFailure(name, sdkgo.FailureAvailability, "discount creation outcome is unknown"), exchange.retryAfter)
	}
	if exchange.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PaddleDiscount{}, *discountFailure(name, sdkgo.FailureTransport, "an earlier discount create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CreateDiscountBranchProviderRejected, PaddleDiscount{}, discountFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected discount creation"), sdkgo.Receipt{})
	}
	discount, ok := decodePaddleDiscount(exchange.body, "")
	if !ok || exchange.err != nil || !strings.EqualFold(discount.Code, input.Code) {
		return sdkgo.NewMutationUncertain(PaddleDiscount{}, *discountFailure(name, sdkgo.FailureProtocol, "Paddle accepted discount creation but returned invalid data"), sdkgo.Receipt{})
	}
	_ = fence.RecordProviderID(discount.ID)
	return sdkgo.NewMutationBranch(CreateDiscountBranchCreated, discount, nil, sdkgo.Receipt{ProviderObjectID: discount.ID})
}

func (client *Client) replaceDiscount(call sdkgo.Call, operationName string, discountID string, payload any, expectedStatus string, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[PaddleDiscount] {
	exchange := client.exchange(call, call.Context, http.MethodPatch, "/discounts/"+url.PathEscape(discountID), payload)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(defectBranch, PaddleDiscount{}, discountFailure(operationName, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddleDiscount](*discountFailure(operationName, sdkgo.FailureAvailability, "discount update is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(notFoundBranch, PaddleDiscount{}, discountFailure(operationName, sdkgo.FailureNotFound, "discount was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewMutationBranch(rejectedBranch, PaddleDiscount{}, discountFailure(operationName, sdkgo.FailureProviderRejection, "Paddle rejected discount update"), sdkgo.Receipt{})
	}
	discount, ok := decodePaddleDiscount(exchange.body, discountID)
	if !ok || exchange.err != nil || expectedStatus != "" && discount.Status != expectedStatus {
		return sdkgo.NewMutationBranch(invalidBranch, PaddleDiscount{}, discountFailure(operationName, sdkgo.FailureProtocol, "Paddle returned an invalid discount"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(successBranch, discount, nil, sdkgo.Receipt{ProviderObjectID: discount.ID})
}

// Invoke replaces a discount's description and percentage amount.
func (operation UpdateDiscountOperation) Invoke(call sdkgo.Call, input UpdateDiscountInput) sdkgo.MutationAttempt[PaddleDiscount] {
	const name = "updateDiscount"
	if !validPaddleID(input.DiscountID, "dsc_") || !validDiscountDescription(input.Description) || !validPercentageAmount(input.Amount) {
		return sdkgo.NewMutationBranch(UpdateDiscountBranchDefect, PaddleDiscount{}, discountFailure(name, sdkgo.FailureValidation, "discount ID, description, or percentage is invalid"), sdkgo.Receipt{})
	}
	return operation.client.replaceDiscount(call, name, input.DiscountID, map[string]string{"description": input.Description, "amount": input.Amount}, "", UpdateDiscountBranchUpdated, UpdateDiscountBranchNotFound, UpdateDiscountBranchProviderRejected, UpdateDiscountBranchInvalidResponse, UpdateDiscountBranchDefect)
}

// Invoke archives a discount with an absolute status replacement.
func (operation ArchiveDiscountOperation) Invoke(call sdkgo.Call, input DiscountIDInput) sdkgo.MutationAttempt[PaddleDiscount] {
	const name = "archiveDiscount"
	if !validPaddleID(input.DiscountID, "dsc_") {
		return sdkgo.NewMutationBranch(ArchiveDiscountBranchDefect, PaddleDiscount{}, discountFailure(name, sdkgo.FailureValidation, "discount ID is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.replaceDiscount(call, name, input.DiscountID, map[string]string{"status": "archived"}, "archived", ArchiveDiscountBranchArchived, ArchiveDiscountBranchNotFound, ArchiveDiscountBranchProviderRejected, ArchiveDiscountBranchInvalidResponse, ArchiveDiscountBranchDefect)
	return result
}
