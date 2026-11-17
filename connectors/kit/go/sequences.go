// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListSequencesInput selects a cursor page of Kit sequences.
type ListSequencesInput struct {
	// PerPage bounds the page size; zero uses 20.
	PerPage int `json:"perPage,omitempty"`
	// After is the prior page's opaque end cursor.
	After string `json:"after,omitempty"`
}

// ListSequenceSubscribersInput selects a cursor page of one sequence's subscribers.
type ListSequenceSubscribersInput struct {
	// SequenceID is the numeric Kit sequence ID.
	SequenceID int64 `json:"sequenceId"`
	// PerPage bounds the page size; zero uses 20.
	PerPage int `json:"perPage,omitempty"`
	// After is the prior page's opaque end cursor.
	After string `json:"after,omitempty"`
}

// AddSubscriberToSequenceInput identifies a sequence and existing subscriber.
type AddSubscriberToSequenceInput struct {
	// SequenceID is the numeric Kit sequence ID.
	SequenceID int64 `json:"sequenceId"`
	// SubscriberID is an existing subscriber's numeric ID.
	SubscriberID int64 `json:"subscriberId"`
}

// SequenceMetadata contains a sequence's identity and delivery settings.
type SequenceMetadata struct {
	// ID is the numeric Kit sequence ID.
	ID int64 `json:"id"`
	// Name is the display name.
	Name string `json:"name"`
	// Active reports whether the sequence currently delivers.
	Active bool `json:"active"`
	// Repeat reports whether subscribers may re-enter after completion.
	Repeat bool `json:"repeat"`
}

// SequencesPage contains one cursor page of Kit sequences.
type SequencesPage struct {
	// Sequences are the returned sequences.
	Sequences []SequenceMetadata `json:"sequences"`
	// NextCursor is nonempty when another page exists.
	NextCursor string `json:"nextCursor"`
}

// ListSequencesOperation lists email sequences.
type ListSequencesOperation struct{ client *Client }

// ListSequenceSubscribersOperation lists subscribers of one sequence.
type ListSequenceSubscribersOperation struct{ client *Client }

// AddSubscriberToSequenceOperation adds an existing subscriber to a sequence.
type AddSubscriberToSequenceOperation struct{ client *Client }

// ListSequences binds sequence listing to this client.
func (client *Client) ListSequences() ListSequencesOperation { return ListSequencesOperation{client} }

// ListSequenceSubscribers binds sequence subscriber listing to this client.
func (client *Client) ListSequenceSubscribers() ListSequenceSubscribersOperation {
	return ListSequenceSubscribersOperation{client}
}

// AddSubscriberToSequence binds sequence enrollment to this client.
func (client *Client) AddSubscriberToSequence() AddSubscriberToSequenceOperation {
	return AddSubscriberToSequenceOperation{client}
}

// Definition returns the generated query contract.
func (ListSequencesOperation) Definition() sdkgo.QueryDefinition { return ListSequencesDefinition }

// Definition returns the generated query contract.
func (ListSequenceSubscribersOperation) Definition() sdkgo.QueryDefinition {
	return ListSequenceSubscribersDefinition
}

// Definition returns the generated mutation contract.
func (AddSubscriberToSequenceOperation) Definition() sdkgo.MutationDefinition {
	return AddSubscriberToSequenceDefinition
}

// IdempotencyKey derives a stable call identity across retries.
func (AddSubscriberToSequenceOperation) IdempotencyKey(callID sdkgo.CallID, _ AddSubscriberToSequenceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one cursor page of email sequences.
func (operation ListSequencesOperation) Invoke(call sdkgo.Call, input ListSequencesInput) sdkgo.QueryAttempt[SequencesPage] {
	branches := queryBranches{found: ListSequencesBranchListed, rejected: ListSequencesBranchProviderRejected, invalid: ListSequencesBranchInvalidResponse, defect: ListSequencesBranchDefect}
	if input.PerPage < 0 || input.PerPage > 1000 || len(input.After) > 2048 {
		return queryBranch("listSequences", branches.defect, SequencesPage{}, sdkgo.FailureValidation, "cursor or perPage is invalid")
	}
	if input.PerPage == 0 {
		input.PerPage = 20
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return invokeQuery(call, operation.client, "listSequences", "/sequences?"+query.Encode(), branches, decodeSequencesPage)
}

// Invoke reads one cursor page of a sequence's subscribers.
func (operation ListSequenceSubscribersOperation) Invoke(call sdkgo.Call, input ListSequenceSubscribersInput) sdkgo.QueryAttempt[SubscribersPage] {
	branches := queryBranches{found: ListSequenceSubscribersBranchListed, notFound: ListSequenceSubscribersBranchNotFound, rejected: ListSequenceSubscribersBranchProviderRejected, invalid: ListSequenceSubscribersBranchInvalidResponse, defect: ListSequenceSubscribersBranchDefect}
	if input.SequenceID <= 0 || input.PerPage < 0 || input.PerPage > 1000 || len(input.After) > 2048 {
		return queryBranch("listSequenceSubscribers", branches.defect, SubscribersPage{}, sdkgo.FailureValidation, "sequence ID, cursor, or perPage is invalid")
	}
	if input.PerPage == 0 {
		input.PerPage = 20
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	path := "/sequences/" + strconv.FormatInt(input.SequenceID, 10) + "/subscribers?" + query.Encode()
	return invokeQuery(call, operation.client, "listSequenceSubscribers", path, branches, func(body []byte) (SubscribersPage, error) {
		return decodeSubscribersPage(body, ListSubscribersInput{After: input.After, PerPage: input.PerPage})
	})
}

// Invoke enrolls an existing subscriber with a bounded send count.
func (operation AddSubscriberToSequenceOperation) Invoke(call sdkgo.Call, input AddSubscriberToSequenceInput) sdkgo.MutationAttempt[SubscriberMetadata] {
	if input.SequenceID <= 0 || input.SubscriberID <= 0 {
		return kitMutationBranch("addSubscriberToSequence", AddSubscriberToSequenceBranchDefect, SubscriberMetadata{}, sdkgo.FailureValidation, "positive sequence and subscriber IDs are required")
	}
	path := "/sequences/" + strconv.FormatInt(input.SequenceID, 10) + "/subscribers/" + strconv.FormatInt(input.SubscriberID, 10)
	fence, err := singledispatch.NewFence(call, AddSubscriberToSequenceDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return kitMutationBranch("addSubscriberToSequence", AddSubscriberToSequenceBranchDefect, SubscriberMetadata{}, sdkgo.FailureLocalDefect, "sequence dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SubscriberMetadata{}, kitFailure("addSubscriberToSequence", sdkgo.FailureTransport, "an earlier sequence enrollment may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var responseBody []byte
	var status int
	var retryAfterDuration = time.Duration(0)
	var requestError error
	err = fence.Dispatch(func(requestContext context.Context) error {
		responseBody, status, retryAfterDuration, requestError = operation.client.requestWithContext(requestContext, call, http.MethodPost, path, []byte("{}"))
		return requestError
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SubscriberMetadata](kitFailure("addSubscriberToSequence", sdkgo.FailureAvailability, "sequence dispatch could not start"), 0)
	}
	if errors.Is(requestError, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SubscriberMetadata{}, kitFailure("addSubscriberToSequence", sdkgo.FailureAuthentication, "earlier sequence enrollment may have succeeded"), sdkgo.Receipt{})
		}
		return kitMutationBranch("addSubscriberToSequence", AddSubscriberToSequenceBranchDefect, SubscriberMetadata{}, sdkgo.FailureAuthentication, "Kit credentials are unavailable")
	}
	if status == 429 || status == 503 {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[SubscriberMetadata](kitFailure("addSubscriberToSequence", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[SubscriberMetadata](kitFailure("addSubscriberToSequence", sdkgo.FailureAvailability, "Kit did not process sequence enrollment"), retryAfterDuration)
	}
	if errors.Is(requestError, errRequest) || status == 408 || status >= 500 || requestError != nil {
		failure := kitFailure("addSubscriberToSequence", sdkgo.FailureTransport, "sequence enrollment outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[SubscriberMetadata](failure, retryAfterDuration)
		}
		return sdkgo.NewMutationUncertain(SubscriberMetadata{}, failure, sdkgo.Receipt{})
	}
	if status == 404 {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SubscriberMetadata{}, kitFailure("addSubscriberToSequence", sdkgo.FailureNotFound, "earlier enrollment may have succeeded"), sdkgo.Receipt{})
		}
		return kitMutationBranch("addSubscriberToSequence", AddSubscriberToSequenceBranchNotFound, SubscriberMetadata{}, sdkgo.FailureNotFound, "Kit did not find the sequence or subscriber")
	}
	if status != 200 && status != 201 {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SubscriberMetadata{}, kitFailure("addSubscriberToSequence", sdkgo.FailureProviderRejection, "earlier enrollment may have succeeded"), sdkgo.Receipt{})
		}
		return kitMutationBranch("addSubscriberToSequence", AddSubscriberToSequenceBranchProviderRejected, SubscriberMetadata{}, sdkgo.FailureProviderRejection, "Kit rejected sequence enrollment")
	}
	subscriber, err := decodeSubscriber(responseBody)
	if err != nil {
		return sdkgo.NewMutationUncertain(SubscriberMetadata{}, kitFailure("addSubscriberToSequence", sdkgo.FailureProtocol, "sequence enrollment cannot be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(AddSubscriberToSequenceBranchAdded, subscriber, nil, sdkgo.Receipt{})
}

func decodeSequencesPage(body []byte) (SequencesPage, error) {
	var wire struct {
		Sequences  []SequenceMetadata `json:"sequences"`
		Pagination *struct {
			HasNextPage bool   `json:"has_next_page"`
			EndCursor   string `json:"end_cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Sequences == nil || wire.Pagination == nil {
		return SequencesPage{}, fmt.Errorf("invalid sequence page")
	}
	page := SequencesPage{Sequences: wire.Sequences}
	if wire.Pagination.HasNextPage {
		if wire.Pagination.EndCursor == "" {
			return SequencesPage{}, fmt.Errorf("missing sequence cursor")
		}
		page.NextCursor = wire.Pagination.EndCursor
	}
	for _, sequence := range page.Sequences {
		if sequence.ID <= 0 || sequence.Name == "" {
			return SequencesPage{}, fmt.Errorf("invalid sequence metadata")
		}
	}
	return page, nil
}
