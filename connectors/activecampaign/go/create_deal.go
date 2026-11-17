// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateDealInput creates a deal for one primary contact in a pipeline stage.
type CreateDealInput struct {
	// Title is the deal title used in read-back after an unknown outcome.
	Title string `json:"title"`
	// ContactID is the numeric primary contact ID.
	ContactID string `json:"contactId"`
	// PipelineID is the numeric deal pipeline ID.
	PipelineID string `json:"pipelineId"`
	// StageID is the numeric stage ID in the pipeline.
	StageID string `json:"stageId"`
	// ValueMinor is the deal value in the currency's minor unit.
	ValueMinor int64 `json:"valueMinor"`
	// Currency is a three-letter ISO currency code.
	Currency string `json:"currency"`
}

// CreateDealOperation creates one deal with a bounded number of sends.
type CreateDealOperation struct{ client *Client }

type createDealDispatch struct {
	client       *Client
	call         sdkgo.Call
	body         []byte
	responseBody []byte
	status       int
	retryAfter   time.Duration
	err          error
}

// CreateDeal binds the create Mutation to this client.
func (client *Client) CreateDeal() CreateDealOperation { return CreateDealOperation{client} }

// Definition returns the manifest-generated create contract.
func (CreateDealOperation) Definition() sdkgo.MutationDefinition { return CreateDealDefinition }

// IdempotencyKey correlates bounded sends locally; ActiveCampaign does not receive this key.
func (CreateDealOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDealInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads back a possible prior create before bounded resends.
func (operation CreateDealOperation) Invoke(call sdkgo.Call, input CreateDealInput) sdkgo.MutationAttempt[DealMetadata] {
	if !validCreateDealInput(input) {
		return dealMutationBranch("createDeal", CreateDealBranchDefect, sdkgo.FailureValidation, "deal title, contact, pipeline, stage, value, or currency is invalid")
	}
	encoded, err := json.Marshal(map[string]any{"deal": map[string]any{"title": input.Title, "contact": input.ContactID, "group": input.PipelineID, "stage": input.StageID, "value": input.ValueMinor, "currency": strings.ToLower(input.Currency), "status": 0}})
	if err != nil {
		return dealMutationBranch("createDeal", CreateDealBranchDefect, sdkgo.FailureLocalDefect, "deal create could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateDealDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return dealMutationBranch("createDeal", CreateDealBranchDefect, sdkgo.FailureLocalDefect, "deal create checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return dealMutationBranch("createDeal", CreateDealBranchDefect, sdkgo.FailureLocalDefect, "deal create checkpoint is invalid")
	}
	if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
		found, state := operation.readBackDeal(call, input, decision.Marker.ProviderID)
		switch state {
		case "found":
			return sdkgo.NewMutationBranch(CreateDealBranchCreated, found, nil, dealReceipt(call, found.ID))
		case "retry":
			return sdkgo.NewMutationRetry[DealMetadata](queryFailure("createDeal", sdkgo.FailureAvailability, "deal read-back is temporarily unavailable"), 0)
		case "uncertain":
			return createDealUncertain(call, "deal read-back could not uniquely confirm the earlier create")
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return createDealUncertain(call, "an earlier deal create may have succeeded and reached the send cap")
	}
	dispatch := &createDealDispatch{client: operation.client, call: call, body: encoded}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[DealMetadata](queryFailure("createDeal", sdkgo.FailureAvailability, "deal create checkpoint is unavailable"), 0)
	}
	if errors.Is(dispatch.err, errCredential) {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[DealMetadata](queryFailure("createDeal", sdkgo.FailureAvailability, "deal retry checkpoint is unavailable"), 0)
		}
		return dealMutationBranch("createDeal", CreateDealBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(dispatch.err, errRequest) {
		return sdkgo.NewMutationRetry[DealMetadata](queryFailure("createDeal", sdkgo.FailureTransport, "deal create outcome is unknown"), 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[DealMetadata](queryFailure("createDeal", sdkgo.FailureAvailability, "deal retry checkpoint is unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if dispatch.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[DealMetadata](queryFailure("createDeal", kind, "ActiveCampaign temporarily rejected the deal create"), dispatch.retryAfter)
	}
	if dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		return sdkgo.NewMutationRetry[DealMetadata](queryFailure("createDeal", sdkgo.FailureAvailability, "deal create outcome is unknown"), dispatch.retryAfter)
	}
	if dispatch.status != http.StatusCreated && dispatch.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return createDealUncertain(call, "an earlier deal create may have succeeded despite this rejection")
		}
		return dealMutationBranch("createDeal", CreateDealBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(dispatch.status))
	}
	if dispatch.err != nil {
		return createDealUncertain(call, "ActiveCampaign accepted the deal create but response was incomplete")
	}
	deal, err := decodeDeal(dispatch.responseBody)
	if err != nil {
		return createDealUncertain(call, "ActiveCampaign accepted the deal create but returned invalid JSON")
	}
	_ = fence.RecordProviderID(deal.ID)
	return sdkgo.NewMutationBranch(CreateDealBranchCreated, deal, nil, dealReceipt(call, deal.ID))
}

func (operation CreateDealOperation) readBackDeal(call sdkgo.Call, input CreateDealInput, providerID string) (DealMetadata, string) {
	if providerID != "" {
		body, status, _, err := operation.client.get(call, "/deals/"+providerID)
		if err != nil || status == http.StatusTooManyRequests || status >= 500 {
			return DealMetadata{}, "retry"
		}
		if status == http.StatusNotFound {
			return DealMetadata{}, "absent"
		}
		if status != http.StatusOK {
			return DealMetadata{}, "uncertain"
		}
		deal, err := decodeDeal(body)
		if err != nil || !matchesCreatedDeal(deal, input) {
			return DealMetadata{}, "uncertain"
		}
		return deal, "found"
	}
	query := url.Values{"filters[title]": {input.Title}}
	body, status, _, err := operation.client.get(call, "/deals?"+query.Encode())
	if err != nil || status == http.StatusTooManyRequests || status >= 500 {
		return DealMetadata{}, "retry"
	}
	if status != http.StatusOK {
		return DealMetadata{}, "uncertain"
	}
	page, err := decodeDealsPage(body, ListDealsInput{Limit: 20})
	if err != nil {
		return DealMetadata{}, "uncertain"
	}
	var matched DealMetadata
	for _, deal := range page.Deals {
		if matchesCreatedDeal(deal, input) {
			if matched.ID != "" {
				return DealMetadata{}, "uncertain"
			}
			matched = deal
		}
	}
	if page.Total > len(page.Deals) {
		return DealMetadata{}, "uncertain"
	}
	if matched.ID != "" {
		return matched, "found"
	}
	return DealMetadata{}, "absent"
}

func matchesCreatedDeal(deal DealMetadata, input CreateDealInput) bool {
	return deal.Title == input.Title && deal.ContactID == input.ContactID && deal.StageID == input.StageID && deal.ValueMinor == input.ValueMinor && strings.EqualFold(deal.Currency, input.Currency) && deal.Status == 0
}

func validCreateDealInput(input CreateDealInput) bool {
	return strings.TrimSpace(input.Title) != "" && len(input.Title) <= 250 && numericID.MatchString(input.ContactID) && numericID.MatchString(input.PipelineID) && numericID.MatchString(input.StageID) && input.ValueMinor >= 0 && validDealCurrency(input.Currency)
}

func validDealCurrency(currency string) bool {
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

func (dispatch *createDealDispatch) execute(ctx context.Context) error {
	dispatch.responseBody, dispatch.status, dispatch.retryAfter, dispatch.err = dispatch.client.requestWithContext(ctx, dispatch.call, http.MethodPost, "/deals", dispatch.body)
	return nil
}

func createDealUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[DealMetadata] {
	return sdkgo.NewMutationUncertain(DealMetadata{}, queryFailure("createDeal", sdkgo.FailureTransport, message), dealReceipt(call, ""))
}

func dealReceipt(call sdkgo.Call, providerID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "activecampaign", ProviderObjectID: providerID, ObservedAt: time.Now().UTC()}
}

func dealMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DealMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, DealMetadata{}, &failure, sdkgo.Receipt{})
}
