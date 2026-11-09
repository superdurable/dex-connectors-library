// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shortcut

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const maxSafeStoryID uint64 = 9007199254740991

// GetStoryInput selects a Story by its positive decimal public ID.
type GetStoryInput struct {
	// StoryID is the non-secret decimal ID from a Shortcut Story URL.
	StoryID string `json:"storyId"`
}

// StoryMetadata excludes descriptions, comments, owners, and other private fields.
type StoryMetadata struct {
	// StoryID is the public decimal Story ID.
	StoryID string `json:"storyId"`
	// Name is the Story title.
	Name string `json:"name"`
	// StoryType is Shortcut's feature, bug, or chore classification.
	StoryType string `json:"storyType"`
	// WorkflowStateID is the decimal ID of the current workflow state.
	WorkflowStateID string `json:"workflowStateId"`
	// Archived reports whether the Story is archived.
	Archived bool `json:"archived"`
}

// GetStoryOperation reads one Story without exposing private descriptions or comments.
type GetStoryOperation struct{ client *Client }

// GetStory binds the Query to this client.
func (client *Client) GetStory() GetStoryOperation { return GetStoryOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetStoryOperation) Definition() sdkgo.QueryDefinition { return GetStoryDefinition }

// Invoke reads one Story and classifies retryable and definitive outcomes.
func (operation GetStoryOperation) Invoke(call sdkgo.Call, input GetStoryInput) sdkgo.QueryAttempt[StoryMetadata] {
	if _, ok := parseStoryID(input.StoryID); !ok {
		return storyBranch(GetStoryBranchDefect, sdkgo.FailureValidation, "story ID must be a positive decimal ID within the safe integer range")
	}
	response := operation.client.get(call, "/stories/"+input.StoryID)
	if errors.Is(response.err, errCredential) {
		return storyBranch(GetStoryBranchDefect, sdkgo.FailureAuthentication, "Shortcut credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[StoryMetadata](storyFailure(sdkgo.FailureTransport, "Shortcut request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[StoryMetadata](storyFailure(sdkgo.FailureRateLimit, "Shortcut rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[StoryMetadata](storyFailure(sdkgo.FailureAvailability, "Shortcut is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return storyBranch(GetStoryBranchNotFound, sdkgo.FailureNotFound, "Shortcut did not find the story")
	}
	if response.status != http.StatusOK {
		return storyBranch(GetStoryBranchProviderRejected, sdkgo.FailureProviderRejection, "Shortcut rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return storyBranch(GetStoryBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Shortcut response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[StoryMetadata](storyFailure(sdkgo.FailureTransport, "Shortcut response was interrupted"), 0)
	}
	story, err := decodeStory(response.body, input.StoryID)
	if err != nil {
		return storyBranch(GetStoryBranchInvalidResponse, sdkgo.FailureProtocol, "Shortcut returned invalid story JSON")
	}
	return sdkgo.NewQueryBranch(GetStoryBranchFound, story, nil, sdkgo.Receipt{})
}

func parseStoryID(storyID string) (uint64, bool) {
	if storyID == "" || storyID[0] == '0' {
		return 0, false
	}
	for _, digit := range storyID {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseUint(storyID, 10, 64)
	return id, err == nil && id > 0 && id <= maxSafeStoryID
}

func decodeStory(body []byte, requestedStoryID string) (StoryMetadata, error) {
	var wire struct {
		ID              int64  `json:"id"`
		Name            string `json:"name"`
		StoryType       string `json:"story_type"`
		WorkflowStateID int64  `json:"workflow_state_id"`
		Archived        *bool  `json:"archived"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return StoryMetadata{}, err
	}
	if wire.ID <= 0 || wire.ID > int64(maxSafeStoryID) || strconv.FormatInt(wire.ID, 10) != requestedStoryID || wire.Name == "" || wire.StoryType == "" || wire.WorkflowStateID <= 0 || wire.WorkflowStateID > int64(maxSafeStoryID) || wire.Archived == nil {
		return StoryMetadata{}, fmt.Errorf("story identity or fields missing")
	}
	return StoryMetadata{StoryID: requestedStoryID, Name: wire.Name, StoryType: wire.StoryType, WorkflowStateID: strconv.FormatInt(wire.WorkflowStateID, 10), Archived: *wire.Archived}, nil
}

func storyFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "shortcut", Operation: "getStory", Message: message}
}

func storyBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[StoryMetadata] {
	failure := storyFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, StoryMetadata{}, &failure, sdkgo.Receipt{})
}
