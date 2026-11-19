// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Posting contains selected non-content fields of a Lever job posting.
type Posting struct {
	// ID is the posting UID.
	ID string `json:"id"`
	// Title is the job posting's text title.
	Title string `json:"title"`
	// State is Lever's current posting state.
	State string `json:"state"`
}

// GetPostingInput identifies one job posting.
type GetPostingInput struct {
	ID string `json:"id"`
}

// PostingsPage contains selected posting fields from one list response.
type PostingsPage struct {
	Postings []Posting `json:"postings"`
}

// ListPostingsInput filters postings by state; blank includes all accessible states.
type ListPostingsInput struct {
	State string `json:"state,omitempty"`
}

// CreatePostingInput supplies the required draft posting title, categories, and actor.
type CreatePostingInput struct {
	PerformAsUserID string `json:"performAsUserId"`
	Title           string `json:"title"`
	Team            string `json:"team"`
	Department      string `json:"department"`
	Location        string `json:"location"`
}

// UpdatePostingInput supplies absolute title and state values for one posting.
type UpdatePostingInput struct {
	ID              string `json:"id"`
	PerformAsUserID string `json:"performAsUserId"`
	Title           string `json:"title"`
	State           string `json:"state"`
}

// GetPostingOperation reads one posting.
type GetPostingOperation struct{ client *Client }

// GetPosting binds the Query to this client.
func (client *Client) GetPosting() GetPostingOperation { return GetPostingOperation{client} }

// Definition returns the generated Query contract.
func (GetPostingOperation) Definition() sdkgo.QueryDefinition { return GetPostingDefinition }

// Invoke reads one posting and projects its ID, title, and state.
func (operation GetPostingOperation) Invoke(call sdkgo.Call, input GetPostingInput) sdkgo.QueryAttempt[Posting] {
	if !validOpportunityID(input.ID) {
		return postingQueryBranch[Posting]("getPosting", GetPostingBranchDefect, sdkgo.FailureValidation, "posting ID is invalid")
	}
	return readPostingQuery(operation.client.get(call, "/postings/"+url.PathEscape(input.ID)), "getPosting", GetPostingBranchFound, GetPostingBranchNotFound, GetPostingBranchProviderRejected, GetPostingBranchInvalidResponse, GetPostingBranchDefect, decodePosting)
}

// ListPostingsOperation reads accessible postings.
type ListPostingsOperation struct{ client *Client }

// ListPostings binds the Query to this client.
func (client *Client) ListPostings() ListPostingsOperation { return ListPostingsOperation{client} }

// Definition returns the generated Query contract.
func (ListPostingsOperation) Definition() sdkgo.QueryDefinition { return ListPostingsDefinition }

// Invoke reads selected fields from one posting list.
func (operation ListPostingsOperation) Invoke(call sdkgo.Call, input ListPostingsInput) sdkgo.QueryAttempt[PostingsPage] {
	if input.State != "" && !validPostingState(input.State) {
		return postingQueryBranch[PostingsPage]("listPostings", ListPostingsBranchDefect, sdkgo.FailureValidation, "posting state is invalid")
	}
	path := "/postings"
	if input.State != "" {
		path += "?" + url.Values{"state": {input.State}}.Encode()
	}
	return readPostingQuery(operation.client.get(call, path), "listPostings", ListPostingsBranchListed, "", ListPostingsBranchProviderRejected, ListPostingsBranchInvalidResponse, ListPostingsBranchDefect, decodePostings)
}

// CreatePostingOperation creates one draft posting.
type CreatePostingOperation struct{ client *Client }

// CreatePosting binds the Mutation to this client.
func (client *Client) CreatePosting() CreatePostingOperation { return CreatePostingOperation{client} }

// Definition returns the generated Mutation contract.
func (CreatePostingOperation) Definition() sdkgo.MutationDefinition { return CreatePostingDefinition }

// IdempotencyKey correlates repeats; Lever does not deduplicate posting creates by this key.
func (CreatePostingOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePostingInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one fenced draft creation and bounds repeats when the outcome is unknown.
func (operation CreatePostingOperation) Invoke(call sdkgo.Call, input CreatePostingInput) sdkgo.MutationAttempt[Posting] {
	if !validOpportunityID(input.PerformAsUserID) || !validCandidateName(input.Title) || !validCandidateName(input.Team) || !validCandidateName(input.Department) || !validCandidateName(input.Location) {
		return postingMutationBranch("createPosting", CreatePostingBranchDefect, sdkgo.FailureValidation, "posting title, categories, or actor is invalid", sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreatePostingDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return postingMutationBranch("createPosting", CreatePostingBranchDefect, sdkgo.FailureLocalDefect, "posting create checkpoint could not be initialized", sdkgo.Receipt{})
	}
	decision := fence.Decide()
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever"}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(Posting{}, postingFailure("createPosting", sdkgo.FailureTransport, "posting may have been created at the send cap"), receipt)
	}
	if decision.Marker.ProviderID != "" {
		readback := operation.client.get(call, "/postings/"+url.PathEscape(decision.Marker.ProviderID))
		if readback.status == http.StatusOK && readback.err == nil {
			posting, err := decodePosting(readback.body)
			if err == nil && posting.ID == decision.Marker.ProviderID && posting.Title == input.Title {
				receipt.ProviderObjectID = posting.ID
				return sdkgo.NewMutationBranch(CreatePostingBranchCreated, posting, nil, receipt)
			}
		}
		return sdkgo.NewMutationRetry[Posting](postingFailure("createPosting", sdkgo.FailureAvailability, "created posting read-back is unavailable"), readback.retryAfter)
	}
	payload, err := json.Marshal(struct {
		Text       string `json:"text"`
		State      string `json:"state"`
		Categories struct {
			Team       string `json:"team"`
			Department string `json:"department"`
			Location   string `json:"location"`
		} `json:"categories"`
	}{Text: input.Title, State: "draft", Categories: struct {
		Team       string `json:"team"`
		Department string `json:"department"`
		Location   string `json:"location"`
	}{Team: input.Team, Department: input.Department, Location: input.Location}})
	if err != nil {
		return postingMutationBranch("createPosting", CreatePostingBranchDefect, sdkgo.FailureLocalDefect, "posting request could not be encoded", receipt)
	}
	path := "/postings?" + url.Values{"perform_as": {input.PerformAsUserID}}.Encode()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doWithContext(call, requestContext, http.MethodPost, path, payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[Posting](postingFailure("createPosting", sdkgo.FailureAvailability, "posting send claim was not durable"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[Posting](postingFailure("createPosting", sdkgo.FailureAvailability, "posting retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[Posting](postingFailure("createPosting", kind, "Lever temporarily refused posting creation"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(Posting{}, postingFailure("createPosting", sdkgo.FailureAuthentication, "credentials are unavailable after a possible create"), receipt)
		}
		return postingMutationBranch("createPosting", CreatePostingBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[Posting](postingFailure("createPosting", sdkgo.FailureTransport, "posting create outcome is unknown"), response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(Posting{}, postingFailure("createPosting", sdkgo.FailureTransport, "posting create outcome is unknown at the send cap"), receipt)
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(Posting{}, postingFailure("createPosting", sdkgo.FailureProviderRejection, "Lever rejected a resend after a possible create"), receipt)
		}
		return postingMutationBranch("createPosting", CreatePostingBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected posting creation", receipt)
	}
	posting, err := decodePosting(response.body)
	if err != nil || posting.Title != input.Title {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[Posting](postingFailure("createPosting", sdkgo.FailureProtocol, "Lever did not confirm the created posting"), 0)
		}
		return sdkgo.NewMutationUncertain(Posting{}, postingFailure("createPosting", sdkgo.FailureProtocol, "Lever did not confirm posting at the send cap"), receipt)
	}
	if err := fence.RecordProviderID(posting.ID); err != nil {
		return sdkgo.NewMutationRetry[Posting](postingFailure("createPosting", sdkgo.FailureAvailability, "posting ID checkpoint failed"), 0)
	}
	receipt.ProviderObjectID = posting.ID
	return sdkgo.NewMutationBranch(CreatePostingBranchCreated, posting, nil, receipt)
}

// UpdatePostingOperation sets a posting's title and state.
type UpdatePostingOperation struct{ client *Client }

// UpdatePosting binds the Mutation to this client.
func (client *Client) UpdatePosting() UpdatePostingOperation { return UpdatePostingOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdatePostingOperation) Definition() sdkgo.MutationDefinition { return UpdatePostingDefinition }

// IdempotencyKey correlates retries of absolute posting updates.
func (UpdatePostingOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePostingInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the absolute title and state and verifies Lever's response.
func (operation UpdatePostingOperation) Invoke(call sdkgo.Call, input UpdatePostingInput) sdkgo.MutationAttempt[Posting] {
	if !validOpportunityID(input.ID) || !validOpportunityID(input.PerformAsUserID) || !validCandidateName(input.Title) || !validPostingState(input.State) {
		return postingMutationBranch("updatePosting", UpdatePostingBranchDefect, sdkgo.FailureValidation, "posting ID, title, state, or actor is invalid", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.ID}
	payload, err := json.Marshal(struct {
		Text  string `json:"text"`
		State string `json:"state"`
	}{Text: input.Title, State: input.State})
	if err != nil {
		return postingMutationBranch("updatePosting", UpdatePostingBranchDefect, sdkgo.FailureLocalDefect, "posting update could not be encoded", receipt)
	}
	path := "/postings/" + url.PathEscape(input.ID) + "?" + url.Values{"perform_as": {input.PerformAsUserID}}.Encode()
	response := operation.client.do(call, http.MethodPost, path, payload)
	if errors.Is(response.err, errCredential) {
		return postingMutationBranch("updatePosting", UpdatePostingBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[Posting](postingFailure("updatePosting", sdkgo.FailureTransport, "posting update outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[Posting](postingFailure("updatePosting", sdkgo.FailureRateLimit, "Lever rate limited posting update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[Posting](postingFailure("updatePosting", sdkgo.FailureAvailability, "posting update outcome is unknown"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return postingMutationBranch("updatePosting", UpdatePostingBranchNotFound, sdkgo.FailureNotFound, "posting was not found", receipt)
	}
	if response.status != http.StatusOK {
		return postingMutationBranch("updatePosting", UpdatePostingBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected posting update", receipt)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationRetry[Posting](postingFailure("updatePosting", sdkgo.FailureResponseTooLarge, "posting update response exceeded the limit after a possible update"), 0)
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[Posting](postingFailure("updatePosting", sdkgo.FailureTransport, "posting response was interrupted"), 0)
	}
	posting, err := decodePosting(response.body)
	if err != nil || posting.ID != input.ID || posting.Title != input.Title || posting.State != input.State {
		return sdkgo.NewMutationRetry[Posting](postingFailure("updatePosting", sdkgo.FailureProtocol, "posting update response did not confirm state"), 0)
	}
	return sdkgo.NewMutationBranch(UpdatePostingBranchUpdated, posting, nil, receipt)
}

func validPostingState(state string) bool {
	switch state {
	case "published", "internal", "closed", "draft", "pending", "rejected":
		return true
	}
	return false
}

func decodePosting(body []byte) (Posting, error) {
	var wire struct {
		Data struct {
			ID    string `json:"id"`
			Text  string `json:"text"`
			State string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || !validOpportunityID(wire.Data.ID) || strings.TrimSpace(wire.Data.Text) == "" || !validPostingState(wire.Data.State) {
		return Posting{}, fmt.Errorf("Lever posting JSON is invalid")
	}
	return Posting{ID: wire.Data.ID, Title: wire.Data.Text, State: wire.Data.State}, nil
}

func decodePostings(body []byte) (PostingsPage, error) {
	var wire struct {
		Data []struct {
			ID    string `json:"id"`
			Text  string `json:"text"`
			State string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil {
		return PostingsPage{}, fmt.Errorf("Lever postings JSON is invalid")
	}
	postings := make([]Posting, 0, len(wire.Data))
	for _, entry := range wire.Data {
		if !validOpportunityID(entry.ID) || strings.TrimSpace(entry.Text) == "" || !validPostingState(entry.State) {
			return PostingsPage{}, fmt.Errorf("Lever posting is invalid")
		}
		postings = append(postings, Posting{ID: entry.ID, Title: entry.Text, State: entry.State})
	}
	return PostingsPage{Postings: postings}, nil
}

func readPostingQuery[Output any](response providerResponse, operation string, found, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (Output, error)) sdkgo.QueryAttempt[Output] {
	if errors.Is(response.err, errCredential) {
		return postingQueryBranch[Output](operation, defect, sdkgo.FailureAuthentication, "Lever credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](postingFailure(operation, sdkgo.FailureTransport, "Lever request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](postingFailure(operation, sdkgo.FailureRateLimit, "Lever rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](postingFailure(operation, sdkgo.FailureAvailability, "Lever is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return postingQueryBranch[Output](operation, notFound, sdkgo.FailureNotFound, "Lever did not find the resource")
	}
	if response.status != http.StatusOK {
		return postingQueryBranch[Output](operation, rejected, sdkgo.FailureProviderRejection, "Lever rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return postingQueryBranch[Output](operation, invalid, sdkgo.FailureResponseTooLarge, "Lever response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](postingFailure(operation, sdkgo.FailureTransport, "Lever response was interrupted"), 0)
	}
	value, err := decode(response.body)
	if err != nil {
		return postingQueryBranch[Output](operation, invalid, sdkgo.FailureProtocol, "Lever returned invalid resource JSON")
	}
	return sdkgo.NewQueryBranch(found, value, nil, sdkgo.Receipt{})
}

func postingFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: operation, Message: message}
}
func postingQueryBranch[Output any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := postingFailure(operation, kind, message)
	var zero Output
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func postingMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[Posting] {
	failure := postingFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, Posting{}, &failure, receipt)
}
