// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elevenlabs

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListHistoryItemsInput selects a bounded page of generated items.
type ListHistoryItemsInput struct {
	// PageSize is from 1 to 1000; zero uses ElevenLabs' default.
	PageSize int `json:"pageSize,omitempty"`
	// StartAfterHistoryItemID resumes after an ID from the preceding page.
	StartAfterHistoryItemID string `json:"startAfterHistoryItemId,omitempty"`
	// VoiceID optionally filters history to one voice.
	VoiceID string `json:"voiceId,omitempty"`
	// ModelID optionally filters to one model and requires Source.
	ModelID string `json:"modelId,omitempty"`
	// Source is TTS, STS, or Flows; blank includes all sources.
	Source string `json:"source,omitempty"`
}

// HistoryItemIdentityInput selects one generated item by provider ID.
type HistoryItemIdentityInput struct {
	// HistoryItemID is returned by ListHistoryItems.
	HistoryItemID string `json:"historyItemId"`
}

// HistoryItemMetadata excludes generated text, audio, and request contents.
type HistoryItemMetadata struct {
	// HistoryItemID is the stable generated item ID.
	HistoryItemID string `json:"historyItemId"`
	// DateUnix is the creation time in Unix seconds.
	DateUnix int64 `json:"dateUnix"`
	// VoiceID identifies the voice when one was used.
	VoiceID string `json:"voiceId,omitempty"`
	// ModelID identifies the model when one was used.
	ModelID string `json:"modelId,omitempty"`
	// ContentType is the generated media MIME type.
	ContentType string `json:"contentType"`
	// Source identifies the generation family.
	Source string `json:"source,omitempty"`
}

// HistoryItemsPage contains one page of generated item metadata.
type HistoryItemsPage struct {
	// Items contains operational metadata without generated content.
	Items []HistoryItemMetadata `json:"items"`
	// HasMore reports whether another page is available.
	HasMore bool `json:"hasMore"`
	// LastHistoryItemID selects the next page when HasMore is true.
	LastHistoryItemID string `json:"lastHistoryItemId,omitempty"`
}

// DeletedHistoryItem confirms one generated item is absent.
type DeletedHistoryItem struct {
	// HistoryItemID is the selected provider ID.
	HistoryItemID string `json:"historyItemId"`
	// Deleted is true after deletion or confirmed absence.
	Deleted bool `json:"deleted"`
}

// ListHistoryItemsOperation reads one generated item page.
type ListHistoryItemsOperation struct{ client *Client }

// GetHistoryItemOperation reads one generated item.
type GetHistoryItemOperation struct{ client *Client }

// DeleteHistoryItemOperation deletes one generated item.
type DeleteHistoryItemOperation struct{ client *Client }

// ListHistoryItems binds the page Query to this client.
func (client *Client) ListHistoryItems() ListHistoryItemsOperation {
	return ListHistoryItemsOperation{client}
}

// GetHistoryItem binds the item Query to this client.
func (client *Client) GetHistoryItem() GetHistoryItemOperation {
	return GetHistoryItemOperation{client}
}

// DeleteHistoryItem binds the deletion Mutation to this client.
func (client *Client) DeleteHistoryItem() DeleteHistoryItemOperation {
	return DeleteHistoryItemOperation{client}
}

// Definition returns the generated history page contract.
func (ListHistoryItemsOperation) Definition() sdkgo.QueryDefinition {
	return ListHistoryItemsDefinition
}

// Definition returns the generated item retrieval contract.
func (GetHistoryItemOperation) Definition() sdkgo.QueryDefinition { return GetHistoryItemDefinition }

// Definition returns the generated item deletion contract.
func (DeleteHistoryItemOperation) Definition() sdkgo.MutationDefinition {
	return DeleteHistoryItemDefinition
}

// IdempotencyKey records the Call ID for repeatable deletion.
func (DeleteHistoryItemOperation) IdempotencyKey(callID sdkgo.CallID, _ HistoryItemIdentityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one generated item page without returning text or media.
func (operation ListHistoryItemsOperation) Invoke(call sdkgo.Call, input ListHistoryItemsInput) sdkgo.QueryAttempt[HistoryItemsPage] {
	branches := queryBranches{found: ListHistoryItemsBranchListed, rejected: ListHistoryItemsBranchProviderRejected, invalid: ListHistoryItemsBranchInvalidResponse, defect: ListHistoryItemsBranchDefect}
	if input.PageSize < 0 || input.PageSize > 1000 || input.StartAfterHistoryItemID != "" && !voiceIDPattern.MatchString(input.StartAfterHistoryItemID) || input.VoiceID != "" && !voiceIDPattern.MatchString(input.VoiceID) || input.ModelID != "" && !voiceIDPattern.MatchString(input.ModelID) || input.ModelID != "" && input.Source == "" || input.Source != "" && input.Source != "TTS" && input.Source != "STS" && input.Source != "Flows" {
		return queryBranch("listHistoryItems", branches.defect, HistoryItemsPage{}, sdkgo.FailureValidation, "history page filters are invalid")
	}
	query := url.Values{}
	if input.PageSize > 0 {
		query.Set("page_size", strconv.Itoa(input.PageSize))
	}
	if input.StartAfterHistoryItemID != "" {
		query.Set("start_after_history_item_id", input.StartAfterHistoryItemID)
	}
	if input.VoiceID != "" {
		query.Set("voice_id", input.VoiceID)
	}
	if input.ModelID != "" {
		query.Set("model_id", input.ModelID)
	}
	if input.Source != "" {
		query.Set("source", input.Source)
	}
	return invokeJSONQuery(call, operation.client, "listHistoryItems", branches, http.MethodGet, "/v1/history", query, nil, decodeHistoryItemsPage)
}

// Invoke reads one generated item's operational metadata.
func (operation GetHistoryItemOperation) Invoke(call sdkgo.Call, input HistoryItemIdentityInput) sdkgo.QueryAttempt[HistoryItemMetadata] {
	branches := queryBranches{found: GetHistoryItemBranchFound, notFound: GetHistoryItemBranchNotFound, rejected: GetHistoryItemBranchProviderRejected, invalid: GetHistoryItemBranchInvalidResponse, defect: GetHistoryItemBranchDefect}
	if !voiceIDPattern.MatchString(input.HistoryItemID) {
		return queryBranch("getHistoryItem", branches.defect, HistoryItemMetadata{}, sdkgo.FailureValidation, "history item ID is invalid")
	}
	return invokeJSONQuery(call, operation.client, "getHistoryItem", branches, http.MethodGet, "/v1/history/"+url.PathEscape(input.HistoryItemID), nil, nil, func(body []byte) (HistoryItemMetadata, error) {
		item, err := decodeHistoryItemMetadata(body)
		if err != nil || item.HistoryItemID != input.HistoryItemID {
			return HistoryItemMetadata{}, fmt.Errorf("history item identity is invalid")
		}
		return item, nil
	})
}

// Invoke permanently deletes one generated item; a missing item is already absent.
func (operation DeleteHistoryItemOperation) Invoke(call sdkgo.Call, input HistoryItemIdentityInput) sdkgo.MutationAttempt[DeletedHistoryItem] {
	if !voiceIDPattern.MatchString(input.HistoryItemID) {
		return deleteHistoryFailure(DeleteHistoryItemBranchDefect, sdkgo.FailureValidation, "history item ID is invalid")
	}
	body, status, retryAfter, err := operation.client.do(call, http.MethodDelete, "/v1/history/"+url.PathEscape(input.HistoryItemID), nil, nil)
	if errors.Is(err, errCredential) {
		return deleteHistoryFailure(DeleteHistoryItemBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if errors.Is(err, errRequest) {
		return deleteHistoryRetry(sdkgo.FailureTransport, "history deletion request failed", 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500 {
		kind := sdkgo.FailureAvailability
		if status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return deleteHistoryRetry(kind, "history deletion is unavailable", retryAfter)
	}
	if status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteHistoryItemBranchDeleted, DeletedHistoryItem{HistoryItemID: input.HistoryItemID, Deleted: true}, nil, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return deleteHistoryFailure(DeleteHistoryItemBranchProviderRejected, sdkgo.FailureProviderRejection, "ElevenLabs rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return deleteHistoryFailure(DeleteHistoryItemBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "deletion confirmation exceeded the response limit")
	}
	if err != nil {
		return deleteHistoryRetry(sdkgo.FailureTransport, "history deletion response was interrupted", 0)
	}
	var wire struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Status != "ok" {
		return deleteHistoryFailure(DeleteHistoryItemBranchInvalidResponse, sdkgo.FailureProtocol, "history deletion confirmation is invalid")
	}
	return sdkgo.NewMutationBranch(DeleteHistoryItemBranchDeleted, DeletedHistoryItem{HistoryItemID: input.HistoryItemID, Deleted: true}, nil, sdkgo.Receipt{})
}

func decodeHistoryItemsPage(body []byte) (HistoryItemsPage, error) {
	var wire struct {
		History           []json.RawMessage `json:"history"`
		HasMore           *bool             `json:"has_more"`
		LastHistoryItemID string            `json:"last_history_item_id"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return HistoryItemsPage{}, err
	}
	if wire.History == nil || wire.HasMore == nil || *wire.HasMore && !voiceIDPattern.MatchString(wire.LastHistoryItemID) {
		return HistoryItemsPage{}, fmt.Errorf("history page is incomplete")
	}
	page := HistoryItemsPage{Items: make([]HistoryItemMetadata, 0, len(wire.History)), HasMore: *wire.HasMore, LastHistoryItemID: wire.LastHistoryItemID}
	for _, raw := range wire.History {
		item, err := decodeHistoryItemMetadata(raw)
		if err != nil {
			return HistoryItemsPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

func decodeHistoryItemMetadata(body []byte) (HistoryItemMetadata, error) {
	var wire struct {
		ID          string `json:"history_item_id"`
		DateUnix    int64  `json:"date_unix"`
		VoiceID     string `json:"voice_id"`
		ModelID     string `json:"model_id"`
		ContentType string `json:"content_type"`
		Source      string `json:"source"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return HistoryItemMetadata{}, err
	}
	if !voiceIDPattern.MatchString(wire.ID) || wire.DateUnix <= 0 || strings.TrimSpace(wire.ContentType) == "" || wire.VoiceID != "" && !voiceIDPattern.MatchString(wire.VoiceID) || wire.ModelID != "" && !voiceIDPattern.MatchString(wire.ModelID) {
		return HistoryItemMetadata{}, fmt.Errorf("history item metadata is incomplete")
	}
	return HistoryItemMetadata{HistoryItemID: wire.ID, DateUnix: wire.DateUnix, VoiceID: wire.VoiceID, ModelID: wire.ModelID, ContentType: wire.ContentType, Source: wire.Source}, nil
}

func deleteHistoryFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedHistoryItem] {
	failure := queryFailure("deleteHistoryItem", kind, message)
	return sdkgo.NewMutationBranch(branch, DeletedHistoryItem{}, &failure, sdkgo.Receipt{})
}
func deleteHistoryRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[DeletedHistoryItem] {
	return sdkgo.NewMutationRetry[DeletedHistoryItem](queryFailure("deleteHistoryItem", kind, message), retryAfter)
}
