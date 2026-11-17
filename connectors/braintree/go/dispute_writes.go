// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const acceptDisputeGraphQL = "mutation AcceptDispute($input: AcceptDisputeInput!) { acceptDispute(input: $input) { dispute { id status replyByDate } } }"
const finalizeDisputeGraphQL = "mutation FinalizeDispute($input: FinalizeDisputeInput!) { finalizeDispute(input: $input) { dispute { id status replyByDate } } }"
const createDisputeTextEvidenceGraphQL = "mutation CreateDisputeTextEvidence($input: CreateDisputeTextEvidenceInput!) { createDisputeTextEvidence(input: $input) { evidence { id category content } } }"
const deleteDisputeEvidenceGraphQL = "mutation DeleteDisputeEvidence($input: DeleteDisputeEvidenceInput!) { deleteDisputeEvidence(input: $input) { clientMutationId } }"

// DisputeTextEvidenceProfile is the newly attached text evidence.
type DisputeTextEvidenceProfile = DisputeEvidenceProfile

// CreateDisputeTextEvidenceInput attaches one text statement to an open dispute.
type CreateDisputeTextEvidenceInput struct {
	// DisputeID is the GraphQL dispute ID.
	DisputeID string `json:"disputeId"`
	// Category is a Braintree DisputeTextEvidenceCategory enum.
	Category string `json:"category"`
	// Content is the statement; never put credentials in evidence.
	Content string `json:"content"`
}

// DeleteDisputeEvidenceInput identifies one attached evidence item.
type DeleteDisputeEvidenceInput struct {
	// DisputeID is the owning GraphQL dispute ID.
	DisputeID string `json:"disputeId"`
	// EvidenceID is the attached evidence GraphQL ID.
	EvidenceID string `json:"evidenceId"`
}

// DisputeEvidenceDeleteResult confirms evidence removal.
type DisputeEvidenceDeleteResult struct {
	// DisputeID is the owning dispute.
	DisputeID string `json:"disputeId"`
	// EvidenceID is the removed evidence item.
	EvidenceID string `json:"evidenceId"`
	// Deleted is true only on a successful response.
	Deleted bool `json:"deleted"`
}

// AcceptDisputeOperation accepts one open dispute.
type AcceptDisputeOperation struct{ client *Client }

// AcceptDispute binds the Mutation.
func (client *Client) AcceptDispute() AcceptDisputeOperation { return AcceptDisputeOperation{client} }

// Definition returns the generated Mutation contract.
func (AcceptDisputeOperation) Definition() sdkgo.MutationDefinition { return AcceptDisputeDefinition }

// IdempotencyKey records the Dex identity; Braintree has no dispute apiRequestKey.
func (AcceptDisputeOperation) IdempotencyKey(callID sdkgo.CallID, _ GetDisputeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke accepts a dispute after checking any previous unknown attempt.
func (operation AcceptDisputeOperation) Invoke(call sdkgo.Call, input GetDisputeInput) sdkgo.MutationAttempt[DisputeProfile] {
	return operation.client.applyDisputeStatus(call, input, AcceptDisputeDefinition, "acceptDispute", acceptDisputeGraphQL, "ACCEPTED", AcceptDisputeBranchAccepted, AcceptDisputeBranchNotFound, AcceptDisputeBranchProviderRejected, AcceptDisputeBranchDefect)
}

// FinalizeDisputeOperation submits evidence for one open dispute.
type FinalizeDisputeOperation struct{ client *Client }

// FinalizeDispute binds the Mutation.
func (client *Client) FinalizeDispute() FinalizeDisputeOperation {
	return FinalizeDisputeOperation{client}
}

// Definition returns the generated Mutation contract.
func (FinalizeDisputeOperation) Definition() sdkgo.MutationDefinition {
	return FinalizeDisputeDefinition
}

// IdempotencyKey records the Dex identity; Braintree has no dispute apiRequestKey.
func (FinalizeDisputeOperation) IdempotencyKey(callID sdkgo.CallID, _ GetDisputeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke finalizes a dispute after checking any previous unknown attempt.
func (operation FinalizeDisputeOperation) Invoke(call sdkgo.Call, input GetDisputeInput) sdkgo.MutationAttempt[DisputeProfile] {
	return operation.client.applyDisputeStatus(call, input, FinalizeDisputeDefinition, "finalizeDispute", finalizeDisputeGraphQL, "DISPUTED", FinalizeDisputeBranchFinalized, FinalizeDisputeBranchNotFound, FinalizeDisputeBranchProviderRejected, FinalizeDisputeBranchDefect)
}

func (client *Client) applyDisputeStatus(call sdkgo.Call, input GetDisputeInput, definition sdkgo.MutationDefinition, operation, graphQL, desiredStatus string, success, notFound, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[DisputeProfile] {
	if !validGraphQLID(input.DisputeID) {
		return braintreeMutationBranch[DisputeProfile](operation, defect, sdkgo.FailureValidation, "disputeId is invalid")
	}
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return braintreeMutationBranch[DisputeProfile](operation, defect, sdkgo.FailureLocalDefect, "dispute checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return braintreeMutationUncertain[DisputeProfile](operation, "dispute checkpoint cannot be trusted")
	}
	if decision.MaximumPriorSends > 0 && decision.Marker.State != singledispatch.StateCanResend {
		profile, readErr := client.readDisputeForRecovery(call, input.DisputeID)
		if readErr != nil {
			return braintreeMutationRetry[DisputeProfile](operation, sdkgo.FailureAvailability, "dispute read-back is unavailable", 0)
		}
		if profile.Status == desiredStatus {
			return sdkgo.NewMutationBranch(success, profile, nil, sdkgo.Receipt{})
		}
		if profile.Status != "OPEN" {
			return braintreeMutationUncertain[DisputeProfile](operation, "dispute moved to another status after an unknown send")
		}
	}
	if decision.Kind != singledispatch.SendAllowed {
		return braintreeMutationUncertain[DisputeProfile](operation, "dispute outcome cannot be confirmed after the send cap")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.graphQL(call, requestContext, graphQL, map[string]any{"input": map[string]any{"clientMutationId": string(call.ID), "disputeId": input.DisputeID}})
		return nil
	})
	if err != nil {
		return braintreeMutationRetry[DisputeProfile](operation, sdkgo.FailureAvailability, "dispute dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return braintreeMutationRetry[DisputeProfile](operation, sdkgo.FailureAvailability, "dispute resend checkpoint is unavailable", 0)
		}
	}
	if attempt, handled := classifyBraintreeMutationResponse[DisputeProfile](response, operation, notFound, rejected, "", defect, decision.MaximumPriorSends > 0); handled {
		return attempt
	}
	var wire struct {
		Data map[string]struct {
			Dispute DisputeProfile `json:"dispute"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data[operation].Dispute.ID != input.DisputeID || wire.Data[operation].Dispute.Status != desiredStatus {
		return braintreeMutationRetry[DisputeProfile](operation, sdkgo.FailureProtocol, "Braintree dispute response is uncertain", 0)
	}
	return sdkgo.NewMutationBranch(success, wire.Data[operation].Dispute, nil, sdkgo.Receipt{})
}

// CreateDisputeTextEvidenceOperation attaches one text statement.
type CreateDisputeTextEvidenceOperation struct{ client *Client }

// CreateDisputeTextEvidence binds the Mutation.
func (client *Client) CreateDisputeTextEvidence() CreateDisputeTextEvidenceOperation {
	return CreateDisputeTextEvidenceOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateDisputeTextEvidenceOperation) Definition() sdkgo.MutationDefinition {
	return CreateDisputeTextEvidenceDefinition
}

// IdempotencyKey records the Dex identity; Braintree does not deduplicate evidence creation.
func (CreateDisputeTextEvidenceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDisputeTextEvidenceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke attaches text evidence, reading back before a possible resend.
func (operation CreateDisputeTextEvidenceOperation) Invoke(call sdkgo.Call, input CreateDisputeTextEvidenceInput) sdkgo.MutationAttempt[DisputeTextEvidenceProfile] {
	if !validGraphQLID(input.DisputeID) || !validEvidenceCategory(input.Category) || !validEvidenceContent(input.Content) {
		return braintreeMutationBranch[DisputeTextEvidenceProfile]("createDisputeTextEvidence", CreateDisputeTextEvidenceBranchDefect, sdkgo.FailureValidation, "dispute text evidence input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateDisputeTextEvidenceDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return braintreeMutationBranch[DisputeTextEvidenceProfile]("createDisputeTextEvidence", CreateDisputeTextEvidenceBranchDefect, sdkgo.FailureLocalDefect, "evidence checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return braintreeMutationUncertain[DisputeTextEvidenceProfile]("createDisputeTextEvidence", "evidence checkpoint cannot be trusted")
	}
	if decision.MaximumPriorSends > 0 && decision.Marker.State != singledispatch.StateCanResend {
		profile, readErr := operation.client.readDisputeForRecovery(call, input.DisputeID)
		if readErr != nil {
			return braintreeMutationRetry[DisputeTextEvidenceProfile]("createDisputeTextEvidence", sdkgo.FailureAvailability, "evidence read-back is unavailable", 0)
		}
		for _, evidence := range profile.Evidence {
			if evidence.Category == input.Category && evidence.Content == input.Content && validGraphQLID(evidence.ID) {
				return sdkgo.NewMutationBranch(CreateDisputeTextEvidenceBranchCreated, evidence, nil, sdkgo.Receipt{})
			}
		}
		if profile.Status != "OPEN" {
			return braintreeMutationUncertain[DisputeTextEvidenceProfile]("createDisputeTextEvidence", "dispute moved to another status after an unknown send")
		}
	}
	if decision.Kind != singledispatch.SendAllowed {
		return braintreeMutationUncertain[DisputeTextEvidenceProfile]("createDisputeTextEvidence", "evidence outcome cannot be confirmed after the send cap")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		requestInput := map[string]any{"clientMutationId": string(call.ID), "disputeId": input.DisputeID, "category": input.Category, "content": input.Content}
		response = operation.client.graphQL(call, requestContext, createDisputeTextEvidenceGraphQL, map[string]any{"input": requestInput})
		return nil
	})
	if err != nil {
		return braintreeMutationRetry[DisputeTextEvidenceProfile]("createDisputeTextEvidence", sdkgo.FailureAvailability, "evidence dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return braintreeMutationRetry[DisputeTextEvidenceProfile]("createDisputeTextEvidence", sdkgo.FailureAvailability, "evidence resend checkpoint is unavailable", 0)
		}
	}
	if attempt, handled := classifyBraintreeMutationResponse[DisputeTextEvidenceProfile](response, "createDisputeTextEvidence", CreateDisputeTextEvidenceBranchNotFound, CreateDisputeTextEvidenceBranchProviderRejected, "", CreateDisputeTextEvidenceBranchDefect, decision.MaximumPriorSends > 0); handled {
		return attempt
	}
	var wire struct {
		Data map[string]struct {
			Evidence DisputeTextEvidenceProfile `json:"evidence"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		return braintreeMutationRetry[DisputeTextEvidenceProfile]("createDisputeTextEvidence", sdkgo.FailureProtocol, "Braintree evidence response is uncertain", 0)
	}
	evidence := wire.Data["createDisputeTextEvidence"].Evidence
	if !validGraphQLID(evidence.ID) || evidence.Category != input.Category || evidence.Content != input.Content {
		return braintreeMutationRetry[DisputeTextEvidenceProfile]("createDisputeTextEvidence", sdkgo.FailureProtocol, "Braintree evidence response is uncertain", 0)
	}
	return sdkgo.NewMutationBranch(CreateDisputeTextEvidenceBranchCreated, evidence, nil, sdkgo.Receipt{})
}

// DeleteDisputeEvidenceOperation removes one attached evidence item.
type DeleteDisputeEvidenceOperation struct{ client *Client }

// DeleteDisputeEvidence binds the Mutation.
func (client *Client) DeleteDisputeEvidence() DeleteDisputeEvidenceOperation {
	return DeleteDisputeEvidenceOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteDisputeEvidenceOperation) Definition() sdkgo.MutationDefinition {
	return DeleteDisputeEvidenceDefinition
}

// IdempotencyKey records the stable Dex identity.
func (DeleteDisputeEvidenceOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteDisputeEvidenceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes the selected evidence item by ID.
func (operation DeleteDisputeEvidenceOperation) Invoke(call sdkgo.Call, input DeleteDisputeEvidenceInput) sdkgo.MutationAttempt[DisputeEvidenceDeleteResult] {
	if !validGraphQLID(input.DisputeID) || !validGraphQLID(input.EvidenceID) {
		return braintreeMutationBranch[DisputeEvidenceDeleteResult]("deleteDisputeEvidence", DeleteDisputeEvidenceBranchDefect, sdkgo.FailureValidation, "dispute evidence IDs are invalid")
	}
	requestInput := map[string]any{"clientMutationId": string(call.ID), "disputeId": input.DisputeID, "evidenceId": input.EvidenceID}
	response := operation.client.graphQL(call, call.Context, deleteDisputeEvidenceGraphQL, map[string]any{"input": requestInput})
	if attempt, handled := classifyBraintreeMutationResponse[DisputeEvidenceDeleteResult](response, "deleteDisputeEvidence", DeleteDisputeEvidenceBranchNotFound, DeleteDisputeEvidenceBranchProviderRejected, DeleteDisputeEvidenceBranchInvalidResponse, DeleteDisputeEvidenceBranchDefect, false); handled {
		return attempt
	}
	var wire struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.Data["deleteDisputeEvidence"]) == 0 || string(wire.Data["deleteDisputeEvidence"]) == "null" {
		return braintreeMutationBranch[DisputeEvidenceDeleteResult]("deleteDisputeEvidence", DeleteDisputeEvidenceBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree evidence deletion payload is missing")
	}
	return sdkgo.NewMutationBranch(DeleteDisputeEvidenceBranchDeleted, DisputeEvidenceDeleteResult{DisputeID: input.DisputeID, EvidenceID: input.EvidenceID, Deleted: true}, nil, sdkgo.Receipt{})
}

func (client *Client) readDisputeForRecovery(call sdkgo.Call, disputeID string) (DisputeProfile, error) {
	response := client.getNode(call, getDisputeGraphQL, disputeID)
	if response.err != nil || response.status != http.StatusOK {
		return DisputeProfile{}, errors.New("dispute read-back failed")
	}
	var wire struct {
		Data *struct {
			Node *struct {
				Typename string `json:"__typename"`
				DisputeProfile
			} `json:"node"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.Errors) > 0 || wire.Data == nil || wire.Data.Node == nil || wire.Data.Node.Typename != "Dispute" || wire.Data.Node.ID != disputeID || !validDisputeStatus(wire.Data.Node.Status) {
		return DisputeProfile{}, errors.New("dispute read-back is invalid")
	}
	return wire.Data.Node.DisputeProfile, nil
}

func validEvidenceCategory(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character != '_' && (character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}

func validEvidenceContent(value string) bool {
	return value != "" && len(value) <= 4000 && utf8.ValidString(value) && strings.TrimSpace(value) != ""
}
