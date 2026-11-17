// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateTextNoteInput supplies a text note title and rich-text body.
type CreateTextNoteInput struct {
	// Name is the nonempty note title.
	Name string `json:"name"`
	// Content is Productboard-supported rich text and is never returned in the Result.
	Content string `json:"content"`
}

// CreateTextNoteOperation creates one text note with bounded duplicate sends.
type CreateTextNoteOperation struct{ client *Client }

// CreateTextNote binds the Mutation to this client.
func (client *Client) CreateTextNote() CreateTextNoteOperation {
	return CreateTextNoteOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreateTextNoteOperation) Definition() sdkgo.MutationDefinition { return CreateTextNoteDefinition }

// IdempotencyKey records call identity; Productboard does not deduplicate it.
func (CreateTextNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTextNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a text note and selects uncertainty after an unconfirmed send cap.
func (operation CreateTextNoteOperation) Invoke(call sdkgo.Call, input CreateTextNoteInput) sdkgo.MutationAttempt[NoteMetadata] {
	if !validFeatureName(input.Name) || !utf8.ValidString(input.Content) || len(input.Content) == 0 || len(input.Content) > 100000 {
		return noteCreateBranch(CreateTextNoteBranchDefect, sdkgo.FailureValidation, "note name or content is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateTextNoteDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return noteCreateBranch(CreateTextNoteBranchDefect, sdkgo.FailureLocalDefect, "Productboard dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Marker.ProviderID != "" {
		response := operation.client.get(call, "/notes/"+decision.Marker.ProviderID+"?fields=name")
		if response.status == http.StatusOK && response.err == nil {
			if note, valid := decodeNote(response.body); valid && note.ID == decision.Marker.ProviderID && note.Name == input.Name {
				return sdkgo.NewMutationBranch(CreateTextNoteBranchCreated, note, nil, sdkgo.Receipt{})
			}
		}
		return noteCreateUncertain(sdkgo.FailureTransport, "Productboard note creation could not be confirmed by ID")
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return noteCreateUncertain(sdkgo.FailureTransport, "Productboard note creation could not be confirmed")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return noteCreateBranch(CreateTextNoteBranchDefect, sdkgo.FailureAuthentication, "Productboard credentials are unavailable")
	}
	payload, err := json.Marshal(map[string]any{"data": map[string]any{"type": "textNote", "fields": map[string]string{"name": input.Name, "content": input.Content}}})
	if err != nil {
		return noteCreateBranch(CreateTextNoteBranchDefect, sdkgo.FailureLocalDefect, "Productboard note payload could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/notes", bytes.NewReader(payload))
	if err != nil {
		return noteCreateBranch(CreateTextNoteBranchDefect, sdkgo.FailureLocalDefect, "Productboard note request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIToken.Reveal())
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return noteCreateBranch(CreateTextNoteBranchDefect, sdkgo.FailureLocalDefect, "Productboard note request is invalid")
	}
	dispatch := textNoteDispatch{client: operation.client, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return noteCreateRetry(sdkgo.FailureTransport, "Productboard note request was not sent", 0)
		}
		if !dispatch.didGetConnection.Load() && fence.RecordCanResend() == nil {
			return noteCreateRetry(sdkgo.FailureTransport, "Productboard was unreachable before sending", 0)
		}
		return noteCreateRetry(sdkgo.FailureTransport, "Productboard note outcome is unknown", 0)
	}
	defer func() { _ = dispatch.response.Body.Close() }()
	if dispatch.response.StatusCode == http.StatusTooManyRequests || dispatch.response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return noteCreateRetry(sdkgo.FailureAvailability, "Productboard retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if dispatch.response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return noteCreateRetry(kind, "Productboard did not create the note", providerhttp.ParseRetryAfter(dispatch.response.Header.Get("Retry-After"), time.Now()))
	}
	if dispatch.response.StatusCode >= 500 || dispatch.response.StatusCode == http.StatusRequestTimeout {
		return noteCreateRetry(sdkgo.FailureAvailability, "Productboard note outcome is unknown", 0)
	}
	if dispatch.response.StatusCode != http.StatusCreated {
		return noteCreateBranch(CreateTextNoteBranchProviderRejected, sdkgo.FailureProviderRejection, "Productboard rejected text note creation")
	}
	body, err := providerhttp.ReadBoundedBody(dispatch.response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return noteCreateUncertain(sdkgo.FailureProtocol, "Productboard created the note but its response could not be read")
	}
	noteID, valid := decodeUpdatedNoteID(body)
	if !valid {
		return noteCreateUncertain(sdkgo.FailureProtocol, "Productboard created the note but returned no usable ID")
	}
	if err := fence.RecordProviderID(noteID); err != nil {
		return noteCreateUncertain(sdkgo.FailureLocalDefect, "Productboard note ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(CreateTextNoteBranchCreated, NoteMetadata{ID: noteID, Type: "textNote", Name: input.Name}, nil, sdkgo.Receipt{})
}

type textNoteDispatch struct {
	client           *Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *textNoteDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	request := dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace))
	response, err := dispatch.client.httpClient.Do(request)
	dispatch.response = response
	return err
}

func noteCreateBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[NoteMetadata] {
	failure := featureFailure("createTextNote", kind, message)
	return sdkgo.NewMutationBranch(branch, NoteMetadata{}, &failure, sdkgo.Receipt{})
}

func noteCreateRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[NoteMetadata] {
	return sdkgo.NewMutationRetry[NoteMetadata](featureFailure("createTextNote", kind, message), retryAfter)
}

func noteCreateUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[NoteMetadata] {
	return sdkgo.NewMutationUncertain(NoteMetadata{}, featureFailure("createTextNote", kind, message), sdkgo.Receipt{})
}
