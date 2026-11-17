// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateOpportunityStageInput selects the opportunity and absolute next stage.
type UpdateOpportunityStageInput struct {
	// OpportunityID is the opportunity UID from Lever Opportunities or listOpportunities.
	OpportunityID string `json:"opportunityId"`
	// StageID is a stage UID from Lever Settings > Pipeline and archive reasons.
	StageID string `json:"stageId"`
}

// UpdateOpportunityStageOperation sets an opportunity's absolute stage.
type UpdateOpportunityStageOperation struct{ client *Client }

// UpdateOpportunityStage binds the Mutation to this client.
func (client *Client) UpdateOpportunityStage() UpdateOpportunityStageOperation {
	return UpdateOpportunityStageOperation{client: client}
}

// Definition returns the generated Mutation contract.
func (UpdateOpportunityStageOperation) Definition() sdkgo.MutationDefinition {
	return UpdateOpportunityStageDefinition
}

// IdempotencyKey correlates retries; Lever does not accept a deduplication header for this update.
func (UpdateOpportunityStageOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOpportunityStageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the stage and verifies the resulting state with a read-back.
func (operation UpdateOpportunityStageOperation) Invoke(call sdkgo.Call, input UpdateOpportunityStageInput) sdkgo.MutationAttempt[OpportunityStage] {
	if !validOpportunityID(input.OpportunityID) || !validOpportunityID(input.StageID) {
		return updateStageBranch(UpdateOpportunityStageBranchDefect, sdkgo.FailureValidation, "opportunity and stage IDs must be UUIDs", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.OpportunityID}
	path := "/opportunities/" + url.PathEscape(input.OpportunityID)
	current := operation.client.get(call, path)
	if attempt, isTerminal := classifyStageRead(current, receipt, false); isTerminal {
		return attempt
	}
	currentStage, err := decodeOpportunity(current.body, input.OpportunityID)
	if err != nil {
		return updateStageBranch(UpdateOpportunityStageBranchInvalidResponse, sdkgo.FailureProtocol, "Lever returned invalid opportunity JSON", receipt)
	}
	if currentStage.Stage == input.StageID {
		return sdkgo.NewMutationBranch(UpdateOpportunityStageBranchUpdated, currentStage, nil, receipt)
	}
	payload, err := json.Marshal(struct {
		Stage string `json:"stage"`
	}{Stage: input.StageID})
	if err != nil {
		return updateStageBranch(UpdateOpportunityStageBranchDefect, sdkgo.FailureLocalDefect, "stage request could not be encoded", receipt)
	}
	response := operation.client.do(call, http.MethodPut, path+"/stage", payload)
	if attempt, isTerminal := classifyStageWrite(response, receipt); isTerminal {
		return attempt
	}
	updated := operation.client.get(call, path)
	if attempt, isTerminal := classifyStageRead(updated, receipt, true); isTerminal {
		return attempt
	}
	result, err := decodeOpportunity(updated.body, input.OpportunityID)
	if err != nil {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureProtocol, "Lever stage read-back was invalid after a possible update"), 0)
	}
	if result.Stage != input.StageID {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureAvailability, "Lever has not shown the requested stage yet"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateOpportunityStageBranchUpdated, result, nil, receipt)
}

func classifyStageRead(response providerResponse, receipt sdkgo.Receipt, afterWrite bool) (sdkgo.MutationAttempt[OpportunityStage], bool) {
	if errors.Is(response.err, errCredential) {
		return updateStageBranch(UpdateOpportunityStageBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable or invalid", receipt), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureTransport, "Lever request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureRateLimit, "Lever rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureAvailability, "Lever is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound {
		if afterWrite {
			return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureAvailability, "Lever stage read-back did not find a possibly updated opportunity"), 0), true
		}
		return updateStageBranch(UpdateOpportunityStageBranchNotFound, sdkgo.FailureNotFound, "Lever did not expose the opportunity", receipt), true
	}
	if response.status != http.StatusOK {
		if afterWrite {
			return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureAvailability, "Lever stage read-back failed after a possible update"), response.retryAfter), true
		}
		return updateStageBranch(UpdateOpportunityStageBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected HTTP "+strconv.Itoa(response.status), receipt), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		if afterWrite {
			return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureResponseTooLarge, "Lever stage read-back exceeded the limit after a possible update"), 0), true
		}
		return updateStageBranch(UpdateOpportunityStageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lever response exceeded the configured limit", receipt), true
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureTransport, "Lever response was interrupted"), 0), true
	}
	return sdkgo.MutationAttempt[OpportunityStage]{}, false
}

func classifyStageWrite(response providerResponse, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[OpportunityStage], bool) {
	if errors.Is(response.err, errCredential) {
		return updateStageBranch(UpdateOpportunityStageBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable or invalid", receipt), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureTransport, "Lever stage outcome is unknown"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureRateLimit, "Lever rate limited the stage update"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureAvailability, "Lever stage outcome is unknown"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound {
		return updateStageBranch(UpdateOpportunityStageBranchNotFound, sdkgo.FailureNotFound, "Lever did not expose the opportunity", receipt), true
	}
	if response.status < 200 || response.status >= 300 {
		return updateStageBranch(UpdateOpportunityStageBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected HTTP "+strconv.Itoa(response.status), receipt), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureResponseTooLarge, "Lever stage response exceeded the limit after a possible update"), 0), true
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[OpportunityStage](stageFailure(sdkgo.FailureTransport, "Lever stage response was interrupted"), 0), true
	}
	return sdkgo.MutationAttempt[OpportunityStage]{}, false
}

func stageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: "updateOpportunityStage", Message: message}
}

func updateStageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OpportunityStage] {
	failure := stageFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, OpportunityStage{}, &failure, receipt)
}
