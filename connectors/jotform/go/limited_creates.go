// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jotform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateFormInput supplies a non-secret title for a simple text-response form.
type CreateFormInput struct {
	// Title is the non-empty display title of the new form.
	Title string `json:"title"`
}

// CreateSubmissionInput supplies non-secret answers to one form.
type CreateSubmissionInput struct {
	// FormID is the numeric ID of the target form.
	FormID string `json:"formId"`
	// Answers maps numeric question IDs to non-secret answer text.
	Answers map[string]string `json:"answers"`
}

// CreateFormOperation creates one form with a bounded resend budget.
type CreateFormOperation struct{ client *Client }

// CreateSubmissionOperation creates one submission with a bounded resend budget.
type CreateSubmissionOperation struct{ client *Client }

// CreateForm binds this mutation to the client.
func (client *Client) CreateForm() CreateFormOperation { return CreateFormOperation{client} }

// CreateSubmission binds this mutation to the client.
func (client *Client) CreateSubmission() CreateSubmissionOperation {
	return CreateSubmissionOperation{client}
}

// Definition returns the manifest-generated mutation contract.
func (CreateFormOperation) Definition() sdkgo.MutationDefinition { return CreateFormDefinition }

// Definition returns the manifest-generated mutation contract.
func (CreateSubmissionOperation) Definition() sdkgo.MutationDefinition {
	return CreateSubmissionDefinition
}

// IdempotencyKey keeps the Dex call identity stable across form-create attempts.
func (CreateFormOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateFormInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey keeps the Dex call identity stable across submission-create attempts.
func (CreateSubmissionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSubmissionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one form; an unconfirmed result may be resent within the declared cap.
func (operation CreateFormOperation) Invoke(call sdkgo.Call, input CreateFormInput) sdkgo.MutationAttempt[WriteResult] {
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 256 {
		return writeBranch("createForm", CreateFormBranchDefect, sdkgo.FailureValidation, "bounded form title is required")
	}
	body, err := json.Marshal(map[string]any{
		"properties": map[string]string{"title": input.Title},
		"questions": []map[string]string{
			{"type": "control_head", "text": input.Title, "order": "1", "name": "Header"},
			{"type": "control_textbox", "text": "Answer", "order": "2", "name": "answer", "validation": "None", "required": "No", "readonly": "No", "size": "20", "labelAlign": "Auto", "hint": ""},
		},
	})
	if err != nil {
		return writeBranch("createForm", CreateFormBranchDefect, sdkgo.FailureLocalDefect, "form request could not be encoded")
	}
	return operation.client.invokeLimitedCreate(call, "createForm", http.MethodPut, "/user/forms", body, "application/json", CreateFormDefinition.StepDefaults.ExecuteMethodTimeout, CreateFormBranchCreated, CreateFormBranchProviderRejected, CreateFormBranchUncertain, CreateFormBranchDefect)
}

// Invoke creates one submission; an unconfirmed result may be resent within the declared cap.
func (operation CreateSubmissionOperation) Invoke(call sdkgo.Call, input CreateSubmissionInput) sdkgo.MutationAttempt[WriteResult] {
	if !numericID.MatchString(input.FormID) || len(input.Answers) == 0 {
		return writeBranch("createSubmission", CreateSubmissionBranchDefect, sdkgo.FailureValidation, "numeric form ID and non-empty answers are required")
	}
	form := url.Values{}
	for questionID, answer := range input.Answers {
		if !numericID.MatchString(questionID) || len(answer) > 8192 {
			return writeBranch("createSubmission", CreateSubmissionBranchDefect, sdkgo.FailureValidation, "numeric question IDs and bounded answers are required")
		}
		form.Set("submission["+questionID+"]", answer)
	}
	body := []byte(form.Encode())
	if len(body) > 65536 {
		return writeBranch("createSubmission", CreateSubmissionBranchDefect, sdkgo.FailureValidation, "encoded answers exceed the request limit")
	}
	return operation.client.invokeLimitedCreate(call, "createSubmission", http.MethodPost, "/form/"+input.FormID+"/submissions", body, "application/x-www-form-urlencoded", CreateSubmissionDefinition.StepDefaults.ExecuteMethodTimeout, CreateSubmissionBranchCreated, CreateSubmissionBranchProviderRejected, CreateSubmissionBranchUncertain, CreateSubmissionBranchDefect)
}

func (client *Client) invokeLimitedCreate(call sdkgo.Call, operationName, method, path string, body []byte, contentType string, methodTimeout time.Duration, createdBranch, rejectedBranch, uncertainBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[WriteResult] {
	fence, err := singledispatch.NewFence(call, methodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return writeBranch(operationName, defectBranch, sdkgo.FailureLocalDefect, "Jotform dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(WriteResult{}, writeFailure(operationName, sdkgo.FailureTransport, "an earlier Jotform create may have succeeded"), sdkgo.Receipt{})
	}
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	var responseBody []byte
	var status int
	var retryAfter time.Duration
	var requestErr error
	err = fence.Dispatch(func(requestContext context.Context) error {
		responseBody, status, retryAfter, requestErr = client.requestWithContext(requestContext, call, method, path, body, contentType)
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[WriteResult](writeFailure(operationName, sdkgo.FailureAvailability, "Jotform dispatch could not start safely"), 0)
	}
	if errors.Is(requestErr, errCredential) {
		if hasPossibleEarlierSend {
			return sdkgo.NewMutationUncertain(WriteResult{}, writeFailure(operationName, sdkgo.FailureAuthentication, "an earlier Jotform create may have succeeded, but credentials are unavailable"), sdkgo.Receipt{})
		}
		return writeBranch(operationName, defectBranch, sdkgo.FailureAuthentication, "Jotform credentials are unavailable or invalid")
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[WriteResult](writeFailure(operationName, sdkgo.FailureAvailability, "Jotform retry checkpoint could not be recorded"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[WriteResult](writeFailure(operationName, kind, "Jotform did not process the create"), retryAfter)
	}
	if errors.Is(requestErr, errRequest) || status == http.StatusRequestTimeout || status >= 500 {
		failure := writeFailure(operationName, sdkgo.FailureTransport, "Jotform create outcome is unknown")
		if canResendUnknown {
			return sdkgo.NewMutationRetry[WriteResult](failure, retryAfter)
		}
		return sdkgo.NewMutationUncertain(WriteResult{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		if hasPossibleEarlierSend {
			return sdkgo.NewMutationUncertain(WriteResult{}, writeFailure(operationName, sdkgo.FailureProviderRejection, "an earlier Jotform create may have succeeded"), sdkgo.Receipt{})
		}
		return writeBranch(operationName, rejectedBranch, sdkgo.FailureProviderRejection, "Jotform rejected the create")
	}
	var wire struct {
		ResponseCode int `json:"responseCode"`
		Content      struct {
			ID string `json:"id"`
		} `json:"content"`
	}
	if err := json.Unmarshal(responseBody, &wire); err != nil || wire.ResponseCode == 0 {
		return sdkgo.NewMutationUncertain(WriteResult{}, writeFailure(operationName, sdkgo.FailureProtocol, "Jotform create response cannot be confirmed"), sdkgo.Receipt{})
	}
	if wire.ResponseCode == 429 || wire.ResponseCode >= 500 {
		failure := writeFailure(operationName, sdkgo.FailureAvailability, "Jotform reported an unknown create outcome")
		if canResendUnknown {
			return sdkgo.NewMutationRetry[WriteResult](failure, retryAfter)
		}
		return sdkgo.NewMutationUncertain(WriteResult{}, failure, sdkgo.Receipt{})
	}
	if wire.ResponseCode != 200 {
		if hasPossibleEarlierSend {
			return sdkgo.NewMutationUncertain(WriteResult{}, writeFailure(operationName, sdkgo.FailureProviderRejection, "an earlier Jotform create may have succeeded"), sdkgo.Receipt{})
		}
		return writeBranch(operationName, rejectedBranch, sdkgo.FailureProviderRejection, "Jotform rejected the create")
	}
	if !numericID.MatchString(wire.Content.ID) {
		return sdkgo.NewMutationUncertain(WriteResult{}, writeFailure(operationName, sdkgo.FailureProtocol, "Jotform returned no created resource ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(createdBranch, WriteResult{ID: wire.Content.ID, Accepted: true}, nil, sdkgo.Receipt{})
}
