// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SetOpportunityArchiveStateInput chooses the opportunity and its absolute archive reason.
type SetOpportunityArchiveStateInput struct {
	// OpportunityID is the opportunity UID from Lever Opportunities or listOpportunities.
	OpportunityID string `json:"opportunityId"`
	// ReasonID is an archive reason UID; blank unarchives the opportunity.
	ReasonID string `json:"reasonId"`
}

// OpportunityArchiveState is the provider-confirmed archive state.
type OpportunityArchiveState struct {
	// OpportunityID is the Lever opportunity UID.
	OpportunityID string `json:"opportunityId"`
	// Archived reports whether Lever has archived the opportunity.
	Archived bool `json:"archived"`
	// ReasonID is blank for an active opportunity.
	ReasonID string `json:"reasonId"`
}

// SetOpportunityArchiveStateOperation replaces the archive reason of one opportunity.
type SetOpportunityArchiveStateOperation struct{ client *Client }

// SetOpportunityArchiveState binds the Mutation to this client.
func (client *Client) SetOpportunityArchiveState() SetOpportunityArchiveStateOperation {
	return SetOpportunityArchiveStateOperation{client: client}
}

// Definition returns the generated Mutation contract.
func (SetOpportunityArchiveStateOperation) Definition() sdkgo.MutationDefinition {
	return SetOpportunityArchiveStateDefinition
}

// IdempotencyKey correlates retries; Lever does not accept a deduplication header for this update.
func (SetOpportunityArchiveStateOperation) IdempotencyKey(callID sdkgo.CallID, _ SetOpportunityArchiveStateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the archive reason and verifies the resulting state with a read-back.
func (operation SetOpportunityArchiveStateOperation) Invoke(call sdkgo.Call, input SetOpportunityArchiveStateInput) sdkgo.MutationAttempt[OpportunityArchiveState] {
	if !validOpportunityID(input.OpportunityID) || input.ReasonID != "" && !validOpportunityID(input.ReasonID) {
		return archiveBranch(SetOpportunityArchiveStateBranchDefect, sdkgo.FailureValidation, "opportunity and archive reason IDs must be UUIDs", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.OpportunityID}
	path := "/opportunities/" + url.PathEscape(input.OpportunityID)
	current := operation.client.get(call, path)
	if attempt, terminal := classifyArchiveRead(current, receipt, false); terminal {
		return attempt
	}
	currentState, err := decodeOpportunityArchiveState(current.body, input.OpportunityID)
	if err != nil {
		return archiveBranch(SetOpportunityArchiveStateBranchInvalidResponse, sdkgo.FailureProtocol, "Lever returned invalid opportunity archive JSON", receipt)
	}
	if currentState.ReasonID == input.ReasonID {
		return sdkgo.NewMutationBranch(SetOpportunityArchiveStateBranchUpdated, currentState, nil, receipt)
	}
	var reason *string
	if input.ReasonID != "" {
		reason = &input.ReasonID
	}
	payload, err := json.Marshal(struct {
		Reason *string `json:"reason"`
	}{Reason: reason})
	if err != nil {
		return archiveBranch(SetOpportunityArchiveStateBranchDefect, sdkgo.FailureLocalDefect, "archive request could not be encoded", receipt)
	}
	response := operation.client.do(call, http.MethodPut, path+"/archived", payload)
	if attempt, terminal := classifyArchiveWrite(response, receipt); terminal {
		return attempt
	}
	updated := operation.client.get(call, path)
	if attempt, terminal := classifyArchiveRead(updated, receipt, true); terminal {
		return attempt
	}
	state, err := decodeOpportunityArchiveState(updated.body, input.OpportunityID)
	if err != nil {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureProtocol, "Lever archive read-back was invalid after a possible update"), 0)
	}
	if state.ReasonID != input.ReasonID {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureAvailability, "Lever has not shown the requested archive state yet"), 0)
	}
	return sdkgo.NewMutationBranch(SetOpportunityArchiveStateBranchUpdated, state, nil, receipt)
}

func decodeOpportunityArchiveState(body []byte, opportunityID string) (OpportunityArchiveState, error) {
	var wire struct {
		Data struct {
			ID            string  `json:"id"`
			ArchiveReason *string `json:"archiveReason"`
			ArchivedAt    *int64  `json:"archivedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OpportunityArchiveState{}, err
	}
	if wire.Data.ID != opportunityID {
		return OpportunityArchiveState{}, fmt.Errorf("opportunity identity missing")
	}
	state := OpportunityArchiveState{OpportunityID: opportunityID}
	if wire.Data.ArchiveReason != nil {
		if !validOpportunityID(*wire.Data.ArchiveReason) || wire.Data.ArchivedAt == nil {
			return OpportunityArchiveState{}, fmt.Errorf("invalid archived state")
		}
		state.Archived, state.ReasonID = true, *wire.Data.ArchiveReason
	} else if wire.Data.ArchivedAt != nil {
		return OpportunityArchiveState{}, fmt.Errorf("archive timestamp without reason")
	}
	return state, nil
}

func classifyArchiveRead(response providerResponse, receipt sdkgo.Receipt, afterWrite bool) (sdkgo.MutationAttempt[OpportunityArchiveState], bool) {
	if errors.Is(response.err, errCredential) {
		return archiveBranch(SetOpportunityArchiveStateBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable or invalid", receipt), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureTransport, "Lever request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureRateLimit, "Lever rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureAvailability, "Lever is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound {
		if afterWrite {
			return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureAvailability, "Lever archive read-back did not find a possibly updated opportunity"), 0), true
		}
		return archiveBranch(SetOpportunityArchiveStateBranchNotFound, sdkgo.FailureNotFound, "Lever did not expose the opportunity", receipt), true
	}
	if response.status != http.StatusOK {
		if afterWrite {
			return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureAvailability, "Lever archive read-back failed after a possible update"), response.retryAfter), true
		}
		return archiveBranch(SetOpportunityArchiveStateBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected HTTP "+strconv.Itoa(response.status), receipt), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		if afterWrite {
			return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureResponseTooLarge, "Lever archive read-back exceeded the limit after a possible update"), 0), true
		}
		return archiveBranch(SetOpportunityArchiveStateBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lever response exceeded the configured limit", receipt), true
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureTransport, "Lever response was interrupted"), 0), true
	}
	return sdkgo.MutationAttempt[OpportunityArchiveState]{}, false
}

func classifyArchiveWrite(response providerResponse, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[OpportunityArchiveState], bool) {
	if errors.Is(response.err, errCredential) {
		return archiveBranch(SetOpportunityArchiveStateBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable or invalid", receipt), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureTransport, "Lever archive outcome is unknown"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureRateLimit, "Lever rate limited the archive change"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureAvailability, "Lever archive outcome is unknown"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound {
		return archiveBranch(SetOpportunityArchiveStateBranchNotFound, sdkgo.FailureNotFound, "Lever did not expose the opportunity", receipt), true
	}
	if response.status < 200 || response.status >= 300 {
		return archiveBranch(SetOpportunityArchiveStateBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected HTTP "+strconv.Itoa(response.status), receipt), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureResponseTooLarge, "Lever archive response exceeded the limit after a possible update"), 0), true
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[OpportunityArchiveState](archiveFailure(sdkgo.FailureTransport, "Lever archive response was interrupted"), 0), true
	}
	return sdkgo.MutationAttempt[OpportunityArchiveState]{}, false
}

func archiveFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: "setOpportunityArchiveState", Message: message}
}

func archiveBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OpportunityArchiveState] {
	failure := archiveFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, OpportunityArchiveState{}, &failure, receipt)
}
