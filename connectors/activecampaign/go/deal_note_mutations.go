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
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateDealNoteInput adds text to one deal.
type CreateDealNoteInput struct {
	// DealID is the numeric provider deal ID.
	DealID string `json:"dealId"`
	// Text is the non-empty note body.
	Text string `json:"text"`
}

// UpdateDealNoteInput replaces an existing deal note's text.
type UpdateDealNoteInput struct {
	// DealID is the numeric provider deal ID.
	DealID string `json:"dealId"`
	// NoteID is the numeric provider note ID.
	NoteID string `json:"noteId"`
	// Text is the complete replacement note body.
	Text string `json:"text"`
}

// CreateDealNoteOperation creates one note with bounded sends.
type CreateDealNoteOperation struct{ client *Client }

// UpdateDealNoteOperation replaces an existing note's text.
type UpdateDealNoteOperation struct{ client *Client }

type createDealNoteDispatch struct {
	client       *Client
	call         sdkgo.Call
	path         string
	body         []byte
	responseBody []byte
	status       int
	retryAfter   time.Duration
	err          error
}

// CreateDealNote binds the create Mutation to this client.
func (client *Client) CreateDealNote() CreateDealNoteOperation {
	return CreateDealNoteOperation{client}
}

// UpdateDealNote binds the update Mutation to this client.
func (client *Client) UpdateDealNote() UpdateDealNoteOperation {
	return UpdateDealNoteOperation{client}
}

// Definition returns the manifest-generated create contract.
func (CreateDealNoteOperation) Definition() sdkgo.MutationDefinition { return CreateDealNoteDefinition }

// Definition returns the manifest-generated update contract.
func (UpdateDealNoteOperation) Definition() sdkgo.MutationDefinition { return UpdateDealNoteDefinition }

// IdempotencyKey correlates bounded sends locally; ActiveCampaign does not receive this key.
func (CreateDealNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDealNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable call identity; absolute text replacement is repeat-safe.
func (UpdateDealNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateDealNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads back a possible note before bounded resends.
func (operation CreateDealNoteOperation) Invoke(call sdkgo.Call, input CreateDealNoteInput) sdkgo.MutationAttempt[DealNoteMetadata] {
	if !validDealNoteInput(input.DealID, input.Text) {
		return dealNoteMutationBranch("createDealNote", CreateDealNoteBranchDefect, sdkgo.FailureValidation, "deal ID or note text is invalid")
	}
	encoded, err := json.Marshal(map[string]any{"note": map[string]string{"note": input.Text}})
	if err != nil {
		return dealNoteMutationBranch("createDealNote", CreateDealNoteBranchDefect, sdkgo.FailureLocalDefect, "deal note could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateDealNoteDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return dealNoteMutationBranch("createDealNote", CreateDealNoteBranchDefect, sdkgo.FailureLocalDefect, "note checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return dealNoteMutationBranch("createDealNote", CreateDealNoteBranchDefect, sdkgo.FailureLocalDefect, "note checkpoint is invalid")
	}
	if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
		note, state := operation.readBackDealNote(call, input, decision.Marker.ProviderID)
		switch state {
		case "found":
			return sdkgo.NewMutationBranch(CreateDealNoteBranchCreated, note, nil, dealReceipt(call, note.ID))
		case "retry":
			return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("createDealNote", sdkgo.FailureAvailability, "note read-back is temporarily unavailable"), 0)
		case "uncertain":
			return createDealNoteUncertain(call, "note read-back could not uniquely confirm the earlier create")
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return createDealNoteUncertain(call, "an earlier note create may have succeeded and reached the send cap")
	}
	dispatch := &createDealNoteDispatch{client: operation.client, call: call, path: "/deals/" + input.DealID + "/notes", body: encoded}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("createDealNote", sdkgo.FailureAvailability, "note checkpoint is unavailable"), 0)
	}
	if errors.Is(dispatch.err, errCredential) {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("createDealNote", sdkgo.FailureAvailability, "note retry checkpoint is unavailable"), 0)
		}
		return dealNoteMutationBranch("createDealNote", CreateDealNoteBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(dispatch.err, errRequest) {
		return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("createDealNote", sdkgo.FailureTransport, "note create outcome is unknown"), 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("createDealNote", sdkgo.FailureAvailability, "note retry checkpoint is unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if dispatch.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("createDealNote", kind, "ActiveCampaign temporarily rejected the note create"), dispatch.retryAfter)
	}
	if dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("createDealNote", sdkgo.FailureAvailability, "note create outcome is unknown"), dispatch.retryAfter)
	}
	if dispatch.status != http.StatusCreated && dispatch.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return createDealNoteUncertain(call, "an earlier note create may have succeeded despite this rejection")
		}
		return dealNoteMutationBranch("createDealNote", CreateDealNoteBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(dispatch.status))
	}
	if dispatch.err != nil {
		return createDealNoteUncertain(call, "ActiveCampaign accepted the note but response was incomplete")
	}
	note, err := decodeDealNote(dispatch.responseBody)
	if err != nil || note.DealID != input.DealID {
		return createDealNoteUncertain(call, "ActiveCampaign accepted the note but returned invalid JSON")
	}
	_ = fence.RecordProviderID(note.ID)
	return sdkgo.NewMutationBranch(CreateDealNoteBranchCreated, note, nil, dealReceipt(call, note.ID))
}

// Invoke sets the note's complete text on each attempt.
func (operation UpdateDealNoteOperation) Invoke(call sdkgo.Call, input UpdateDealNoteInput) sdkgo.MutationAttempt[DealNoteMetadata] {
	if !validDealNoteInput(input.DealID, input.Text) || !numericID.MatchString(input.NoteID) {
		return dealNoteMutationBranch("updateDealNote", UpdateDealNoteBranchDefect, sdkgo.FailureValidation, "deal ID, note ID, or text is invalid")
	}
	encoded, err := json.Marshal(map[string]any{"note": map[string]string{"note": input.Text}})
	if err != nil {
		return dealNoteMutationBranch("updateDealNote", UpdateDealNoteBranchDefect, sdkgo.FailureLocalDefect, "deal note could not be encoded")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPut, "/deals/"+input.DealID+"/notes/"+input.NoteID, encoded)
	if errors.Is(err, errCredential) {
		return dealNoteMutationBranch("updateDealNote", UpdateDealNoteBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("updateDealNote", sdkgo.FailureTransport, "ActiveCampaign request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("updateDealNote", sdkgo.FailureRateLimit, "ActiveCampaign rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("updateDealNote", sdkgo.FailureAvailability, "ActiveCampaign is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return dealNoteMutationBranch("updateDealNote", UpdateDealNoteBranchNotFound, sdkgo.FailureNotFound, "ActiveCampaign did not find the note")
	}
	if status != http.StatusOK {
		return dealNoteMutationBranch("updateDealNote", UpdateDealNoteBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return dealNoteMutationBranch("updateDealNote", UpdateDealNoteBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ActiveCampaign response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DealNoteMetadata](queryFailure("updateDealNote", sdkgo.FailureTransport, "ActiveCampaign response was interrupted"), 0)
	}
	note, err := decodeDealNote(body)
	if err != nil || note.DealID != input.DealID {
		return dealNoteMutationBranch("updateDealNote", UpdateDealNoteBranchInvalidResponse, sdkgo.FailureProtocol, "ActiveCampaign returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(UpdateDealNoteBranchUpdated, note, nil, dealReceipt(call, note.ID))
}

func (operation CreateDealNoteOperation) readBackDealNote(call sdkgo.Call, input CreateDealNoteInput, providerID string) (DealNoteMetadata, string) {
	if providerID != "" {
		body, status, _, err := operation.client.get(call, "/notes/"+providerID)
		if err != nil || status == http.StatusTooManyRequests || status >= 500 {
			return DealNoteMetadata{}, "retry"
		}
		if status == http.StatusNotFound {
			return DealNoteMetadata{}, "absent"
		}
		if status != http.StatusOK {
			return DealNoteMetadata{}, "uncertain"
		}
		note, err := decodeDealNote(body)
		if err != nil || note.DealID != input.DealID || note.Text != input.Text {
			return DealNoteMetadata{}, "uncertain"
		}
		return note, "found"
	}
	query := url.Values{"filters[reltype]": {"Deal"}, "filters[relid]": {input.DealID}, "limit": {"100"}, "offset": {"0"}}
	body, status, _, err := operation.client.get(call, "/notes?"+query.Encode())
	if err != nil || status == http.StatusTooManyRequests || status >= 500 {
		return DealNoteMetadata{}, "retry"
	}
	if status != http.StatusOK {
		return DealNoteMetadata{}, "uncertain"
	}
	page, err := decodeDealNotesPage(body, ListDealNotesInput{DealID: input.DealID, Limit: 100})
	if err != nil || page.Total > len(page.Notes) {
		return DealNoteMetadata{}, "uncertain"
	}
	var matched DealNoteMetadata
	for _, note := range page.Notes {
		if note.Text == input.Text {
			if matched.ID != "" {
				return DealNoteMetadata{}, "uncertain"
			}
			matched = note
		}
	}
	if matched.ID != "" {
		return matched, "found"
	}
	return DealNoteMetadata{}, "absent"
}

func validDealNoteInput(dealID, text string) bool {
	return numericID.MatchString(dealID) && strings.TrimSpace(text) != "" && len(text) <= 100000
}

func (dispatch *createDealNoteDispatch) execute(ctx context.Context) error {
	dispatch.responseBody, dispatch.status, dispatch.retryAfter, dispatch.err = dispatch.client.requestWithContext(ctx, dispatch.call, http.MethodPost, dispatch.path, dispatch.body)
	return nil
}

func createDealNoteUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[DealNoteMetadata] {
	return sdkgo.NewMutationUncertain(DealNoteMetadata{}, queryFailure("createDealNote", sdkgo.FailureTransport, message), dealReceipt(call, ""))
}

func dealNoteMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DealNoteMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, DealNoteMetadata{}, &failure, sdkgo.Receipt{})
}
