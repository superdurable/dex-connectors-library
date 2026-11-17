// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shortcut

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SearchStoriesInput selects one bounded page from Shortcut's story search.
type SearchStoriesInput struct {
	// Query is Shortcut search text; it must not be blank.
	Query string `json:"query"`
	// Next is the opaque page token returned by a previous search; blank starts at page one.
	Next string `json:"next,omitempty"`
	// PageSize is the number of stories, from 1 to 250; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
}

// SearchStoriesOutput contains one page of small story records.
type SearchStoriesOutput struct {
	// Stories are the matching story summaries.
	Stories []StoryMetadata `json:"stories"`
	// Next is the opaque token for the next page, or blank at the end.
	Next string `json:"next,omitempty"`
	// Total is Shortcut's reported matching count.
	Total int `json:"total"`
}

// SearchStoriesOperation queries Shortcut's story search endpoint.
type SearchStoriesOperation struct{ client *Client }

// SearchStories binds the search Query to this client.
func (client *Client) SearchStories() SearchStoriesOperation { return SearchStoriesOperation{client} }

// Definition returns the generated search contract.
func (SearchStoriesOperation) Definition() sdkgo.QueryDefinition { return SearchStoriesDefinition }

// Invoke returns a story search page without descriptions or comments.
func (operation SearchStoriesOperation) Invoke(call sdkgo.Call, input SearchStoriesInput) sdkgo.QueryAttempt[SearchStoriesOutput] {
	query := strings.TrimSpace(input.Query)
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 25
	}
	if query == "" || len(query) > 512 || pageSize < 1 || pageSize > 250 || len(input.Next) > 2048 {
		return searchStoriesBranch(SearchStoriesBranchDefect, sdkgo.FailureValidation, "Shortcut search query, page size, or cursor is invalid")
	}
	parameters := url.Values{"query": {query}, "page_size": {strconv.Itoa(pageSize)}, "detail": {"slim"}}
	if input.Next != "" {
		parameters.Set("next", input.Next)
	}
	response := operation.client.get(call, "/search/stories?"+parameters.Encode())
	if errors.Is(response.err, errCredential) {
		return searchStoriesBranch(SearchStoriesBranchDefect, sdkgo.FailureAuthentication, "Shortcut credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SearchStoriesOutput](storyOperationFailure("searchStories", sdkgo.FailureRateLimit, "Shortcut rate limited search"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SearchStoriesOutput](storyOperationFailure("searchStories", sdkgo.FailureAvailability, "Shortcut search is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return searchStoriesBranch(SearchStoriesBranchProviderRejected, sdkgo.FailureProviderRejection, "Shortcut rejected search")
	}
	if response.err != nil {
		if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
			return searchStoriesBranch(SearchStoriesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Shortcut search exceeded the response limit")
		}
		return sdkgo.NewQueryRetry[SearchStoriesOutput](storyOperationFailure("searchStories", sdkgo.FailureTransport, "Shortcut search response was interrupted"), 0)
	}
	var wire struct {
		Data  []json.RawMessage `json:"data"`
		Next  string            `json:"next"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data == nil || wire.Total < 0 {
		return searchStoriesBranch(SearchStoriesBranchInvalidResponse, sdkgo.FailureProtocol, "Shortcut returned invalid search JSON")
	}
	output := SearchStoriesOutput{Stories: make([]StoryMetadata, 0, len(wire.Data)), Next: wire.Next, Total: wire.Total}
	for _, raw := range wire.Data {
		story, err := decodeReturnedStory(raw)
		if err != nil {
			return searchStoriesBranch(SearchStoriesBranchInvalidResponse, sdkgo.FailureProtocol, "Shortcut returned an invalid story")
		}
		output.Stories = append(output.Stories, story)
	}
	return sdkgo.NewQueryBranch(SearchStoriesBranchFound, output, nil, sdkgo.Receipt{})
}

// CreateStoryInput supplies the minimum fields for a new Shortcut story.
type CreateStoryInput struct {
	// Name is the story title, 1 to 512 characters.
	Name string `json:"name"`
	// StoryType is feature, bug, or chore.
	StoryType string `json:"storyType"`
	// WorkflowStateID is the positive decimal ID selected in Shortcut's workflow settings.
	WorkflowStateID string `json:"workflowStateId"`
}

// CreateStoryOperation creates a story with bounded duplicate risk.
type CreateStoryOperation struct{ client *Client }

// CreateStory binds the create Mutation to this client.
func (client *Client) CreateStory() CreateStoryOperation { return CreateStoryOperation{client} }

// Definition returns the generated create contract.
func (CreateStoryOperation) Definition() sdkgo.MutationDefinition { return CreateStoryDefinition }

// IdempotencyKey identifies one Dex call; Shortcut does not document server-side deduplication.
func (CreateStoryOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateStoryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a story through a bounded single-dispatch fence.
func (operation CreateStoryOperation) Invoke(call sdkgo.Call, input CreateStoryInput) sdkgo.MutationAttempt[StoryMetadata] {
	name := strings.TrimSpace(input.Name)
	workflowStateID, isValidState := parseStoryID(input.WorkflowStateID)
	if name == "" || len(name) > 512 || !isValidState || !validStoryType(input.StoryType) {
		return storyMutationBranch(CreateStoryBranchDefect, "createStory", sdkgo.FailureValidation, "Shortcut story name, type, or workflow state ID is invalid")
	}
	payload, err := json.Marshal(map[string]any{"name": name, "story_type": input.StoryType, "workflow_state_id": workflowStateID})
	if err != nil {
		return storyMutationBranch(CreateStoryBranchDefect, "createStory", sdkgo.FailureLocalDefect, "Shortcut story request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateStoryDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return storyMutationBranch(CreateStoryBranchDefect, "createStory", sdkgo.FailureLocalDefect, "Shortcut create checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return storyMutationBranch(CreateStoryBranchDefect, "createStory", sdkgo.FailureLocalDefect, "Shortcut create checkpoint is invalid")
	}
	if decision.Marker.ProviderID != "" {
		response := operation.client.get(call, "/stories/"+url.PathEscape(decision.Marker.ProviderID))
		if response.status == http.StatusOK && response.err == nil {
			story, decodeErr := decodeStory(response.body, decision.Marker.ProviderID)
			if decodeErr == nil && story.Name == name && story.StoryType == input.StoryType && story.WorkflowStateID == input.WorkflowStateID {
				return sdkgo.NewMutationBranch(CreateStoryBranchCreated, story, nil, storyReceipt(call, story.StoryID))
			}
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(StoryMetadata{}, storyOperationFailure("createStory", sdkgo.FailureAvailability, "Shortcut story creation cannot be confirmed"), storyReceipt(call, ""))
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestWithContext(call, requestContext, http.MethodPost, "/stories", payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[StoryMetadata](storyOperationFailure("createStory", sdkgo.FailureAvailability, "Shortcut create dispatch could not be checkpointed"), 0)
	}
	if errors.Is(response.err, errCredential) {
		return storyMutationBranch(CreateStoryBranchDefect, "createStory", sdkgo.FailureAuthentication, "Shortcut credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		_ = fence.RecordCanResend() // A missing checkpoint fails closed on the next attempt.
		return sdkgo.NewMutationRetry[StoryMetadata](storyOperationFailure("createStory", sdkgo.FailureRateLimit, "Shortcut temporarily refused story creation"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[StoryMetadata](storyOperationFailure("createStory", sdkgo.FailureAvailability, "Shortcut story creation outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return storyMutationBranch(CreateStoryBranchProviderRejected, "createStory", sdkgo.FailureProviderRejection, "Shortcut rejected story creation")
	}
	story, decodeErr := decodeReturnedStory(response.body)
	if decodeErr != nil {
		return sdkgo.NewMutationRetry[StoryMetadata](storyOperationFailure("createStory", sdkgo.FailureProtocol, "Shortcut accepted creation but returned no usable story ID"), 0)
	}
	_ = fence.RecordProviderID(story.StoryID) // A lost checkpoint forbids a resend.
	return sdkgo.NewMutationBranch(CreateStoryBranchCreated, story, nil, storyReceipt(call, story.StoryID))
}

// UpdateStoryInput sets absolute fields on one existing story.
type UpdateStoryInput struct {
	// StoryID is the positive decimal public story ID.
	StoryID string `json:"storyId"`
	// Name replaces the title when supplied; blank leaves it unchanged.
	Name string `json:"name,omitempty"`
	// WorkflowStateID replaces the state when supplied; blank leaves it unchanged.
	WorkflowStateID string `json:"workflowStateId,omitempty"`
	// Archived replaces the archived flag when supplied; nil leaves it unchanged.
	Archived *bool `json:"archived,omitempty"`
}

// UpdateStoryOperation sets absolute story fields.
type UpdateStoryOperation struct{ client *Client }

// UpdateStory binds the update Mutation to this client.
func (client *Client) UpdateStory() UpdateStoryOperation { return UpdateStoryOperation{client} }

// Definition returns the generated update contract.
func (UpdateStoryOperation) Definition() sdkgo.MutationDefinition { return UpdateStoryDefinition }

// IdempotencyKey identifies one Dex call; absolute field replacement is safe to repeat.
func (UpdateStoryOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateStoryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke updates one story and classifies definite and retryable responses.
func (operation UpdateStoryOperation) Invoke(call sdkgo.Call, input UpdateStoryInput) sdkgo.MutationAttempt[StoryMetadata] {
	if _, ok := parseStoryID(input.StoryID); !ok {
		return storyMutationBranch(UpdateStoryBranchDefect, "updateStory", sdkgo.FailureValidation, "Shortcut story ID is invalid")
	}
	fields := map[string]any{}
	if input.Name != "" {
		if strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 {
			return storyMutationBranch(UpdateStoryBranchDefect, "updateStory", sdkgo.FailureValidation, "Shortcut story name is invalid")
		}
		fields["name"] = input.Name
	}
	if input.WorkflowStateID != "" {
		stateID, ok := parseStoryID(input.WorkflowStateID)
		if !ok {
			return storyMutationBranch(UpdateStoryBranchDefect, "updateStory", sdkgo.FailureValidation, "Shortcut workflow state ID is invalid")
		}
		fields["workflow_state_id"] = stateID
	}
	if input.Archived != nil {
		fields["archived"] = *input.Archived
	}
	if len(fields) == 0 {
		return storyMutationBranch(UpdateStoryBranchDefect, "updateStory", sdkgo.FailureValidation, "Shortcut update requires at least one field")
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return storyMutationBranch(UpdateStoryBranchDefect, "updateStory", sdkgo.FailureLocalDefect, "Shortcut update could not be encoded")
	}
	response := operation.client.request(call, http.MethodPut, "/stories/"+input.StoryID, payload)
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[StoryMetadata](storyOperationFailure("updateStory", sdkgo.FailureAvailability, "Shortcut update is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return storyMutationBranch(UpdateStoryBranchNotFound, "updateStory", sdkgo.FailureNotFound, "Shortcut did not find the story")
	}
	if errors.Is(response.err, errCredential) {
		return storyMutationBranch(UpdateStoryBranchDefect, "updateStory", sdkgo.FailureAuthentication, "Shortcut credentials are unavailable")
	}
	if response.status != http.StatusOK {
		return storyMutationBranch(UpdateStoryBranchProviderRejected, "updateStory", sdkgo.FailureProviderRejection, "Shortcut rejected the update")
	}
	if response.err != nil {
		return storyMutationBranch(UpdateStoryBranchInvalidResponse, "updateStory", sdkgo.FailureProtocol, "Shortcut update response is invalid")
	}
	story, err := decodeStory(response.body, input.StoryID)
	if err != nil {
		return storyMutationBranch(UpdateStoryBranchInvalidResponse, "updateStory", sdkgo.FailureProtocol, "Shortcut returned invalid story JSON")
	}
	return sdkgo.NewMutationBranch(UpdateStoryBranchUpdated, story, nil, storyReceipt(call, story.StoryID))
}

// DeleteStoryInput identifies a story to delete.
type DeleteStoryInput struct {
	// StoryID is the positive decimal public story ID.
	StoryID string `json:"storyId"`
}

// DeleteStoryOutput confirms the deleted public story ID.
type DeleteStoryOutput struct {
	// StoryID is the deleted story ID.
	StoryID string `json:"storyId"`
}

// DeleteStoryOperation removes a story.
type DeleteStoryOperation struct{ client *Client }

// DeleteStory binds the delete Mutation to this client.
func (client *Client) DeleteStory() DeleteStoryOperation { return DeleteStoryOperation{client} }

// Definition returns the generated delete contract.
func (DeleteStoryOperation) Definition() sdkgo.MutationDefinition { return DeleteStoryDefinition }

// IdempotencyKey identifies one Dex call; deleting the same ID is safe to repeat.
func (DeleteStoryOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteStoryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one story and treats a later 404 as already deleted.
func (operation DeleteStoryOperation) Invoke(call sdkgo.Call, input DeleteStoryInput) sdkgo.MutationAttempt[DeleteStoryOutput] {
	if _, ok := parseStoryID(input.StoryID); !ok {
		return deleteStoryBranch(DeleteStoryBranchDefect, sdkgo.FailureValidation, "Shortcut story ID is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/stories/"+input.StoryID, nil)
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[DeleteStoryOutput](storyOperationFailure("deleteStory", sdkgo.FailureAvailability, "Shortcut delete is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return deleteStoryBranch(DeleteStoryBranchDefect, sdkgo.FailureAuthentication, "Shortcut credentials are unavailable")
	}
	if response.status == http.StatusNotFound && call.Context.Attempt() == 1 {
		return deleteStoryBranch(DeleteStoryBranchNotFound, sdkgo.FailureNotFound, "Shortcut did not find the story")
	}
	if response.status == http.StatusNoContent || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteStoryBranchDeleted, DeleteStoryOutput{StoryID: input.StoryID}, nil, storyReceipt(call, input.StoryID))
	}
	return deleteStoryBranch(DeleteStoryBranchProviderRejected, sdkgo.FailureProviderRejection, "Shortcut rejected the delete")
}

func validStoryType(storyType string) bool {
	return storyType == "feature" || storyType == "bug" || storyType == "chore"
}

func decodeReturnedStory(body []byte) (StoryMetadata, error) {
	var wire struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID <= 0 || wire.ID > int64(maxSafeStoryID) {
		return StoryMetadata{}, errors.New("Shortcut story ID is invalid")
	}
	return decodeStory(body, strconv.FormatInt(wire.ID, 10))
}

func storyOperationFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "shortcut", Operation: operation, Message: message}
}

func storyReceipt(call sdkgo.Call, storyID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "shortcut", ProviderObjectID: storyID}
}

func searchStoriesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SearchStoriesOutput] {
	failure := storyOperationFailure("searchStories", kind, message)
	return sdkgo.NewQueryBranch(branch, SearchStoriesOutput{}, &failure, sdkgo.Receipt{})
}

func storyMutationBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[StoryMetadata] {
	failure := storyOperationFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, StoryMetadata{}, &failure, sdkgo.Receipt{})
}

func deleteStoryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeleteStoryOutput] {
	failure := storyOperationFailure("deleteStory", kind, message)
	return sdkgo.NewMutationBranch(branch, DeleteStoryOutput{}, &failure, sdkgo.Receipt{})
}
