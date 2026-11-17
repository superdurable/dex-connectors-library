// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const keywordsPath = "/CampaignManagement/v13/Keywords"

// KeywordSummary contains fields used to identify one search keyword.
type KeywordSummary struct {
	// ID is the numeric keyword ID.
	ID string `json:"id"`
	// Text is the search phrase.
	Text string `json:"text"`
	// MatchType is Broad, Phrase, or Exact.
	MatchType string `json:"matchType"`
	// Status is Active or Paused.
	Status string `json:"status"`
}

// KeywordsPage contains the keywords in one ad group.
type KeywordsPage struct {
	// Keywords are the returned IDs, phrases, match types, and statuses.
	Keywords []KeywordSummary `json:"keywords"`
}

// ListKeywordsInput identifies the owning ad group.
type ListKeywordsInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
}

// GetKeywordInput identifies one keyword in an ad group.
type GetKeywordInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// KeywordID is the numeric keyword ID.
	KeywordID string `json:"keywordId"`
}

// CreateKeywordInput defines a paused search keyword.
type CreateKeywordInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// Text is the phrase with at most 100 characters.
	Text string `json:"text"`
	// MatchType is Broad, Phrase, or Exact.
	MatchType string `json:"matchType"`
	// Bid optionally overrides the inherited CPC bid in account currency.
	Bid *float64 `json:"bid,omitempty"`
}

// UpdateKeywordInput changes a keyword's status or CPC bid.
type UpdateKeywordInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// KeywordID is the numeric keyword ID.
	KeywordID string `json:"keywordId"`
	// Status changes serving to Active or Paused when nonblank.
	Status string `json:"status,omitempty"`
	// Bid replaces the CPC bid in account currency when supplied.
	Bid *float64 `json:"bid,omitempty"`
}

// DeleteKeywordInput identifies a keyword to remove.
type DeleteKeywordInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// KeywordID is the numeric keyword ID.
	KeywordID string `json:"keywordId"`
}

// KeywordMutationResult identifies the keyword affected by a write.
type KeywordMutationResult struct {
	// KeywordID is the numeric provider keyword ID.
	KeywordID string `json:"keywordId"`
}

// ListKeywordsOperation reads one ad group's keywords.
type ListKeywordsOperation struct{ client *Client }

// GetKeywordOperation reads one keyword.
type GetKeywordOperation struct{ client *Client }

// CreateKeywordOperation creates a paused keyword.
type CreateKeywordOperation struct{ client *Client }

// UpdateKeywordOperation changes a keyword's status or bid.
type UpdateKeywordOperation struct{ client *Client }

// DeleteKeywordOperation removes a keyword.
type DeleteKeywordOperation struct{ client *Client }

// ListKeywords binds the Query to this client.
func (client *Client) ListKeywords() ListKeywordsOperation { return ListKeywordsOperation{client} }

// GetKeyword binds the Query to this client.
func (client *Client) GetKeyword() GetKeywordOperation { return GetKeywordOperation{client} }

// CreateKeyword binds the Mutation to this client.
func (client *Client) CreateKeyword() CreateKeywordOperation { return CreateKeywordOperation{client} }

// UpdateKeyword binds the Mutation to this client.
func (client *Client) UpdateKeyword() UpdateKeywordOperation { return UpdateKeywordOperation{client} }

// DeleteKeyword binds the Mutation to this client.
func (client *Client) DeleteKeyword() DeleteKeywordOperation { return DeleteKeywordOperation{client} }

// Definition returns the generated keyword list Query.
func (ListKeywordsOperation) Definition() sdkgo.QueryDefinition { return ListKeywordsDefinition }

// Definition returns the generated keyword lookup Query.
func (GetKeywordOperation) Definition() sdkgo.QueryDefinition { return GetKeywordDefinition }

// Definition returns the generated keyword create Mutation.
func (CreateKeywordOperation) Definition() sdkgo.MutationDefinition { return CreateKeywordDefinition }

// Definition returns the generated keyword update Mutation.
func (UpdateKeywordOperation) Definition() sdkgo.MutationDefinition { return UpdateKeywordDefinition }

// Definition returns the generated keyword delete Mutation.
func (DeleteKeywordOperation) Definition() sdkgo.MutationDefinition { return DeleteKeywordDefinition }

// IdempotencyKey identifies this bounded create call.
func (CreateKeywordOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateKeywordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies an absolute keyword update.
func (UpdateKeywordOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateKeywordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies the keyword deletion.
func (DeleteKeywordOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteKeywordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists the keywords in an ad group.
func (operation ListKeywordsOperation) Invoke(call sdkgo.Call, input ListKeywordsInput) sdkgo.QueryAttempt[KeywordsPage] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) {
		return keywordQueryBranch(ListKeywordsBranchDefect, KeywordsPage{}, "listKeywords", sdkgo.FailureValidation, "account or ad group ID is invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodPost, keywordsPath+"/QueryByAdGroupId", input.AccountID, map[string]any{"AdGroupId": input.AdGroupID})
	if response.credentialFailure {
		return keywordQueryBranch(ListKeywordsBranchDefect, KeywordsPage{}, "listKeywords", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewQueryRetry[KeywordsPage](campaignFailure("listKeywords", response.failureKind(), "keyword list is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return keywordQueryBranch(ListKeywordsBranchProviderRejected, KeywordsPage{}, "listKeywords", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeKeywords(response.body)
	if response.err != nil || err != nil {
		return keywordQueryBranch(ListKeywordsBranchInvalidResponse, KeywordsPage{}, "listKeywords", sdkgo.FailureProtocol, "Microsoft Advertising returned invalid keywords")
	}
	return sdkgo.NewQueryBranch(ListKeywordsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one keyword by ID.
func (operation GetKeywordOperation) Invoke(call sdkgo.Call, input GetKeywordInput) sdkgo.QueryAttempt[KeywordSummary] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) || !validAdvertisingID(input.KeywordID) {
		return keywordQueryBranch(GetKeywordBranchDefect, KeywordSummary{}, "getKeyword", sdkgo.FailureValidation, "keyword IDs are invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodPost, keywordsPath+"/QueryByIds", input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "KeywordIds": []string{input.KeywordID}})
	if response.credentialFailure {
		return keywordQueryBranch(GetKeywordBranchDefect, KeywordSummary{}, "getKeyword", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewQueryRetry[KeywordSummary](campaignFailure("getKeyword", response.failureKind(), "keyword lookup is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return keywordQueryBranch(GetKeywordBranchProviderRejected, KeywordSummary{}, "getKeyword", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	if isSingleNullEntity(response.body, "Keywords") {
		return sdkgo.NewQueryBranch(GetKeywordBranchNotFound, KeywordSummary{}, nil, sdkgo.Receipt{})
	}
	page, err := decodeKeywords(response.body)
	if response.err != nil || err != nil || len(page.Keywords) > 1 {
		return keywordQueryBranch(GetKeywordBranchInvalidResponse, KeywordSummary{}, "getKeyword", sdkgo.FailureProtocol, "Microsoft Advertising returned invalid keyword JSON")
	}
	if len(page.Keywords) == 0 {
		return sdkgo.NewQueryBranch(GetKeywordBranchNotFound, KeywordSummary{}, nil, sdkgo.Receipt{})
	}
	if page.Keywords[0].ID != input.KeywordID {
		return keywordQueryBranch(GetKeywordBranchInvalidResponse, KeywordSummary{}, "getKeyword", sdkgo.FailureProtocol, "Microsoft Advertising returned a different keyword")
	}
	return sdkgo.NewQueryBranch(GetKeywordBranchFound, page.Keywords[0], nil, sdkgo.Receipt{})
}

// Invoke creates a paused keyword with bounded resends and exact phrase read-back.
func (operation CreateKeywordOperation) Invoke(call sdkgo.Call, input CreateKeywordInput) sdkgo.MutationAttempt[KeywordMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) || strings.TrimSpace(input.Text) != input.Text || input.Text == "" || len(input.Text) > 100 || !validKeywordMatchType(input.MatchType) || (input.Bid != nil && !isPositiveFinite(*input.Bid)) {
		return keywordWriteBranch(CreateKeywordBranchDefect, KeywordMutationResult{}, "createKeyword", sdkgo.FailureValidation, "keyword create input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateKeywordDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return keywordWriteBranch(CreateKeywordBranchDefect, KeywordMutationResult{}, "createKeyword", sdkgo.FailureLocalDefect, "keyword dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return keywordWriteBranch(CreateKeywordBranchDefect, KeywordMutationResult{}, "createKeyword", sdkgo.FailureLocalDefect, "keyword dispatch checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		readBack := operation.client.requestCampaign(call, http.MethodPost, keywordsPath+"/QueryByAdGroupId", input.AccountID, map[string]any{"AdGroupId": input.AdGroupID})
		if readBack.status == http.StatusOK && readBack.err == nil {
			page, decodeErr := decodeKeywords(readBack.body)
			if decodeErr == nil {
				for _, keyword := range page.Keywords {
					if strings.EqualFold(keyword.Text, input.Text) && keyword.MatchType == input.MatchType {
						return sdkgo.NewMutationBranch(CreateKeywordBranchCreated, KeywordMutationResult{KeywordID: keyword.ID}, nil, sdkgo.Receipt{})
					}
				}
			}
		}
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(KeywordMutationResult{}, campaignFailure("createKeyword", sdkgo.FailureAvailability, "keyword create outcome cannot be confirmed"), sdkgo.Receipt{})
	}
	keyword := map[string]any{"Text": input.Text, "MatchType": input.MatchType, "Status": "Paused"}
	if input.Bid != nil {
		keyword["Bid"] = map[string]any{"Amount": *input.Bid}
	}
	var response campaignHTTPResponse
	err = fence.Dispatch(func(dispatchContext context.Context) error {
		response = operation.client.requestCampaignWithContext(dispatchContext, call, http.MethodPost, keywordsPath, input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "Keywords": []any{keyword}})
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[KeywordMutationResult](campaignFailure("createKeyword", sdkgo.FailureTransport, "keyword dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[KeywordMutationResult](campaignFailure("createKeyword", sdkgo.FailureTransport, "keyword retry checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[KeywordMutationResult](campaignFailure("createKeyword", response.failureKind(), "Microsoft Advertising temporarily refused the create"), response.retryAfter)
	}
	if response.credentialFailure {
		return keywordWriteBranch(CreateKeywordBranchDefect, KeywordMutationResult{}, "createKeyword", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[KeywordMutationResult](campaignFailure("createKeyword", sdkgo.FailureAvailability, "keyword create outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return keywordWriteBranch(CreateKeywordBranchProviderRejected, KeywordMutationResult{}, "createKeyword", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	var wire struct {
		KeywordIDs    []string          `json:"KeywordIds"`
		PartialErrors []json.RawMessage `json:"PartialErrors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.KeywordIDs) != 1 || !validAdvertisingID(wire.KeywordIDs[0]) || len(wire.PartialErrors) != 0 {
		return sdkgo.NewMutationUncertain(KeywordMutationResult{}, campaignFailure("createKeyword", sdkgo.FailureProtocol, "keyword create response is incomplete"), sdkgo.Receipt{})
	}
	if err := fence.RecordProviderID(wire.KeywordIDs[0]); err != nil {
		return sdkgo.NewMutationUncertain(KeywordMutationResult{}, campaignFailure("createKeyword", sdkgo.FailureLocalDefect, "keyword checkpoint is unavailable"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateKeywordBranchCreated, KeywordMutationResult{KeywordID: wire.KeywordIDs[0]}, nil, sdkgo.Receipt{})
}

// Invoke replaces a keyword's status or CPC bid.
func (operation UpdateKeywordOperation) Invoke(call sdkgo.Call, input UpdateKeywordInput) sdkgo.MutationAttempt[KeywordMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) || !validAdvertisingID(input.KeywordID) || (input.Status == "" && input.Bid == nil) || (input.Status != "" && input.Status != "Active" && input.Status != "Paused") || (input.Bid != nil && !isPositiveFinite(*input.Bid)) {
		return keywordWriteBranch(UpdateKeywordBranchDefect, KeywordMutationResult{}, "updateKeyword", sdkgo.FailureValidation, "keyword update is invalid")
	}
	keyword := map[string]any{"Id": input.KeywordID}
	if input.Status != "" {
		keyword["Status"] = input.Status
	}
	if input.Bid != nil {
		keyword["Bid"] = map[string]any{"Amount": *input.Bid}
	}
	response := operation.client.requestCampaign(call, http.MethodPut, keywordsPath, input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "Keywords": []any{keyword}})
	return keywordSafeWrite("updateKeyword", UpdateKeywordBranchUpdated, UpdateKeywordBranchProviderRejected, UpdateKeywordBranchInvalidResponse, UpdateKeywordBranchDefect, input.KeywordID, response)
}

// Invoke deletes one keyword by ID.
func (operation DeleteKeywordOperation) Invoke(call sdkgo.Call, input DeleteKeywordInput) sdkgo.MutationAttempt[KeywordMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) || !validAdvertisingID(input.KeywordID) {
		return keywordWriteBranch(DeleteKeywordBranchDefect, KeywordMutationResult{}, "deleteKeyword", sdkgo.FailureValidation, "keyword IDs are invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodDelete, keywordsPath, input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "KeywordIds": []string{input.KeywordID}})
	return keywordSafeWrite("deleteKeyword", DeleteKeywordBranchDeleted, DeleteKeywordBranchProviderRejected, DeleteKeywordBranchInvalidResponse, DeleteKeywordBranchDefect, input.KeywordID, response)
}

func validKeywordMatchType(matchType string) bool {
	return matchType == "Broad" || matchType == "Phrase" || matchType == "Exact"
}

func decodeKeywords(body []byte) (KeywordsPage, error) {
	var wire struct {
		Keywords []struct {
			ID        string `json:"Id"`
			Text      string `json:"Text"`
			MatchType string `json:"MatchType"`
			Status    string `json:"Status"`
		} `json:"Keywords"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Keywords == nil {
		return KeywordsPage{}, fmt.Errorf("keywords are missing or malformed")
	}
	page := KeywordsPage{Keywords: make([]KeywordSummary, 0, len(wire.Keywords))}
	for _, keyword := range wire.Keywords {
		if !validAdvertisingID(keyword.ID) || keyword.Text == "" || !validKeywordMatchType(keyword.MatchType) || (keyword.Status != "Active" && keyword.Status != "Paused" && keyword.Status != "Deleted") {
			return KeywordsPage{}, fmt.Errorf("keyword identity is invalid")
		}
		page.Keywords = append(page.Keywords, KeywordSummary{ID: keyword.ID, Text: keyword.Text, MatchType: keyword.MatchType, Status: keyword.Status})
	}
	return page, nil
}

func keywordQueryBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := campaignFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func keywordWriteBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := campaignFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func keywordSafeWrite(operation string, success, rejection, invalid, defect sdkgo.BranchID, keywordID string, response campaignHTTPResponse) sdkgo.MutationAttempt[KeywordMutationResult] {
	if response.credentialFailure {
		return keywordWriteBranch(defect, KeywordMutationResult{}, operation, sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewMutationRetry[KeywordMutationResult](campaignFailure(operation, response.failureKind(), "keyword write is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return keywordWriteBranch(rejection, KeywordMutationResult{}, operation, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return keywordWriteBranch(invalid, KeywordMutationResult{}, operation, sdkgo.FailureProtocol, "keyword response is unreadable")
	}
	if response.status == http.StatusOK && len(response.body) > 0 {
		var wire struct {
			PartialErrors []json.RawMessage `json:"PartialErrors"`
		}
		if json.Unmarshal(response.body, &wire) != nil {
			return keywordWriteBranch(invalid, KeywordMutationResult{}, operation, sdkgo.FailureProtocol, "keyword response is invalid")
		}
		if len(wire.PartialErrors) != 0 {
			return keywordWriteBranch(rejection, KeywordMutationResult{}, operation, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected the keyword")
		}
	}
	return sdkgo.NewMutationBranch(success, KeywordMutationResult{KeywordID: keywordID}, nil, sdkgo.Receipt{})
}
