// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

var positiveDecimalAmount = regexp.MustCompile(`^(?:[1-9][0-9]*(?:\.[0-9]{1,8})?|0\.[0-9]{0,7}[1-9])$`)

// CreateQuoteInput prices one source or target amount for a profile.
type CreateQuoteInput struct {
	// ProfileID is the positive decimal Wise profile ID.
	ProfileID string `json:"profileId"`
	// SourceCurrency is the source ISO 4217 currency.
	SourceCurrency string `json:"sourceCurrency"`
	// TargetCurrency is the target ISO 4217 currency.
	TargetCurrency string `json:"targetCurrency"`
	// AmountSide is source or target and selects which amount is fixed.
	AmountSide string `json:"amountSide"`
	// Amount is a positive decimal with up to eight fractional digits.
	Amount string `json:"amount"`
}

// CreateQuoteOperation creates an authenticated Wise quote.
type CreateQuoteOperation struct{ client *Client }

// CreateQuote binds the Mutation to this client.
func (client *Client) CreateQuote() CreateQuoteOperation { return CreateQuoteOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateQuoteOperation) Definition() sdkgo.MutationDefinition { return CreateQuoteDefinition }

// IdempotencyKey identifies attempts of one logical quote request within Dex.
func (CreateQuoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateQuoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a quote with a bounded possible-send count and uncertain outcome after the cap.
func (operation CreateQuoteOperation) Invoke(call sdkgo.Call, input CreateQuoteInput) sdkgo.MutationAttempt[QuoteSummary] {
	if !validTransferID(input.ProfileID) || !validCurrency(input.SourceCurrency) || !validCurrency(input.TargetCurrency) || input.SourceCurrency == input.TargetCurrency || (input.AmountSide != "source" && input.AmountSide != "target") || !positiveDecimalAmount.MatchString(input.Amount) {
		return wiseMutationBranch[QuoteSummary]("createQuote", "defect", sdkgo.FailureValidation, "profile, currencies, amount side, or amount is invalid")
	}
	request := map[string]any{"sourceCurrency": input.SourceCurrency, "targetCurrency": input.TargetCurrency}
	if input.AmountSide == "source" {
		request["sourceAmount"] = json.Number(input.Amount)
	} else {
		request["targetAmount"] = json.Number(input.Amount)
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return wiseMutationBranch[QuoteSummary]("createQuote", "defect", sdkgo.FailureLocalDefect, "Wise quote request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateQuoteDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return wiseMutationBranch[QuoteSummary]("createQuote", "defect", sdkgo.FailureLocalDefect, "Wise quote dispatch could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return wiseQuoteRetry(sdkgo.FailureAvailability, 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertainQuote()
	}
	path := "/profiles/" + url.PathEscape(input.ProfileID) + "/quotes"
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.sendWithContext(call, requestContext, http.MethodPost, path, requestBody)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return wiseQuoteRetry(sdkgo.FailureAvailability, 0)
	}
	if err != nil {
		return wiseQuoteRetry(sdkgo.FailureAvailability, 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends == 0 && fence.RecordNotSent() != nil {
			return wiseQuoteRetry(sdkgo.FailureAvailability, 0)
		}
		return wiseQuoteRetry(sdkgo.FailureAuthentication, 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return wiseQuoteRetry(sdkgo.FailureAvailability, 0)
		}
		return wiseQuoteRetry(sdkgo.FailureAvailability, response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || (response.err != nil && response.status == 0) {
		return wiseQuoteRetry(sdkgo.FailureAvailability, response.retryAfter)
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 || !isDefiniteWiseWriteRefusal(response) {
			return wiseQuoteRetry(sdkgo.FailureAvailability, response.retryAfter)
		}
		if fence.RecordCanResend() != nil {
			return wiseQuoteRetry(sdkgo.FailureAvailability, 0)
		}
		return wiseMutationBranch[QuoteSummary]("createQuote", "providerRejected", sdkgo.FailureProviderRejection, "Wise rejected quote creation")
	}
	quote, err := decodeQuote(response.body)
	if err != nil || quote.SourceCurrency != input.SourceCurrency || quote.TargetCurrency != input.TargetCurrency {
		return wiseQuoteRetry(sdkgo.FailureProtocol, 0)
	}
	return sdkgo.NewMutationBranch(CreateQuoteBranchCreated, quote, nil, sdkgo.Receipt{})
}

func isDefiniteWiseWriteRefusal(response providerResponse) bool {
	switch response.status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	if response.err != nil {
		return false
	}
	var rejection struct {
		ID       json.RawMessage `json:"id"`
		Quote    json.RawMessage `json:"quote"`
		Balance  json.RawMessage `json:"balance"`
		Transfer json.RawMessage `json:"transfer"`
		Data     json.RawMessage `json:"data"`
		Errors   []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	return json.Unmarshal(response.body, &rejection) == nil && len(rejection.ID) == 0 && len(rejection.Quote) == 0 && len(rejection.Balance) == 0 && len(rejection.Transfer) == 0 && (len(rejection.Data) == 0 || string(rejection.Data) == "null") && len(rejection.Errors) > 0 && (rejection.Errors[0].Code != "" || rejection.Errors[0].Message != "")
}

func wiseQuoteRetry(kind sdkgo.FailureKind, delay time.Duration) sdkgo.MutationAttempt[QuoteSummary] {
	return sdkgo.NewMutationRetry[QuoteSummary](sdkgo.Failure{Kind: kind, Provider: "wise", Operation: "createQuote", Message: "Wise quote request may be retried"}, delay)
}

func uncertainQuote() sdkgo.MutationAttempt[QuoteSummary] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "wise", Operation: "createQuote", Message: "Wise quote may have been created but could not be identified"}
	return sdkgo.NewMutationUncertain(QuoteSummary{}, failure, sdkgo.Receipt{})
}
