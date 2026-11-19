// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jotform

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// WriteResult records an accepted Jotform write without submission answers.
type WriteResult struct {
	// ID is the provider ID of the affected form or submission.
	ID string `json:"id"`
	// Accepted is true when Jotform accepted the write.
	Accepted bool `json:"accepted"`
}

// UpdateFormTitleInput selects a form and a non-secret display title.
type UpdateFormTitleInput struct {
	// FormID is the numeric form ID.
	FormID string `json:"formId"`
	// Title is the new non-empty form title.
	Title string `json:"title"`
}

// UpdateSubmissionInput selects a submission and non-secret answers to set.
type UpdateSubmissionInput struct {
	// SubmissionID is the numeric submission ID.
	SubmissionID string `json:"submissionId"`
	// Answers maps numeric question IDs to non-secret answer text.
	Answers map[string]string `json:"answers"`
}

// UpdateFormTitleOperation sets a selected form's title.
type UpdateFormTitleOperation struct{ client *Client }

// DeleteFormOperation deletes a form.
type DeleteFormOperation struct{ client *Client }

// UpdateSubmissionOperation sets selected submission answers.
type UpdateSubmissionOperation struct{ client *Client }

// DeleteSubmissionOperation deletes a submission.
type DeleteSubmissionOperation struct{ client *Client }

// UpdateFormTitle binds this mutation to the client.
func (client *Client) UpdateFormTitle() UpdateFormTitleOperation {
	return UpdateFormTitleOperation{client}
}

// DeleteForm binds this mutation to the client.
func (client *Client) DeleteForm() DeleteFormOperation { return DeleteFormOperation{client} }

// UpdateSubmission binds this mutation to the client.
func (client *Client) UpdateSubmission() UpdateSubmissionOperation {
	return UpdateSubmissionOperation{client}
}

// DeleteSubmission binds this mutation to the client.
func (client *Client) DeleteSubmission() DeleteSubmissionOperation {
	return DeleteSubmissionOperation{client}
}

// Definition returns the manifest-generated mutation contract.
func (UpdateFormTitleOperation) Definition() sdkgo.MutationDefinition {
	return UpdateFormTitleDefinition
}

// Definition returns the manifest-generated mutation contract.
func (DeleteFormOperation) Definition() sdkgo.MutationDefinition { return DeleteFormDefinition }

// Definition returns the manifest-generated mutation contract.
func (UpdateSubmissionOperation) Definition() sdkgo.MutationDefinition {
	return UpdateSubmissionDefinition
}

// Definition returns the manifest-generated mutation contract.
func (DeleteSubmissionOperation) Definition() sdkgo.MutationDefinition {
	return DeleteSubmissionDefinition
}

// IdempotencyKey keeps a form update stable across retries.
func (UpdateFormTitleOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateFormTitleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey keeps a form deletion stable across retries.
func (DeleteFormOperation) IdempotencyKey(callID sdkgo.CallID, _ GetFormInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey keeps a submission update stable across retries.
func (UpdateSubmissionOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSubmissionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey keeps a submission deletion stable across retries.
func (DeleteSubmissionOperation) IdempotencyKey(callID sdkgo.CallID, _ GetSubmissionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the form title through Jotform's properties endpoint.
func (operation UpdateFormTitleOperation) Invoke(call sdkgo.Call, input UpdateFormTitleInput) sdkgo.MutationAttempt[WriteResult] {
	if !numericID.MatchString(input.FormID) || strings.TrimSpace(input.Title) == "" || len(input.Title) > 256 {
		return writeBranch("updateFormTitle", UpdateFormTitleBranchDefect, sdkgo.FailureValidation, "numeric form ID and bounded title are required")
	}
	form := url.Values{"properties[title]": {input.Title}}
	return operation.client.invokeSafeWrite(call, "updateFormTitle", http.MethodPost, "/form/"+input.FormID+"/properties", []byte(form.Encode()), "application/x-www-form-urlencoded", input.FormID, safeWriteBranches{success: UpdateFormTitleBranchUpdated, notFound: UpdateFormTitleBranchNotFound, rejected: UpdateFormTitleBranchProviderRejected, invalid: UpdateFormTitleBranchInvalidResponse, defect: UpdateFormTitleBranchDefect})
}

// Invoke deletes one form; repeated deletion has no second effect.
func (operation DeleteFormOperation) Invoke(call sdkgo.Call, input GetFormInput) sdkgo.MutationAttempt[WriteResult] {
	if !numericID.MatchString(input.FormID) {
		return writeBranch("deleteForm", DeleteFormBranchDefect, sdkgo.FailureValidation, "numeric form ID is required")
	}
	return operation.client.invokeSafeWrite(call, "deleteForm", http.MethodDelete, "/form/"+input.FormID, nil, "", input.FormID, safeWriteBranches{success: DeleteFormBranchDeleted, notFound: DeleteFormBranchNotFound, rejected: DeleteFormBranchProviderRejected, invalid: DeleteFormBranchInvalidResponse, defect: DeleteFormBranchDefect})
}

// Invoke sets selected answers; repeating identical field assignments is harmless.
func (operation UpdateSubmissionOperation) Invoke(call sdkgo.Call, input UpdateSubmissionInput) sdkgo.MutationAttempt[WriteResult] {
	if !numericID.MatchString(input.SubmissionID) || len(input.Answers) == 0 {
		return writeBranch("updateSubmission", UpdateSubmissionBranchDefect, sdkgo.FailureValidation, "numeric submission ID and non-empty answers are required")
	}
	form := url.Values{}
	for questionID, answer := range input.Answers {
		if !numericID.MatchString(questionID) || len(answer) > 8192 {
			return writeBranch("updateSubmission", UpdateSubmissionBranchDefect, sdkgo.FailureValidation, "numeric question IDs and bounded answers are required")
		}
		form.Set("submission["+questionID+"]", answer)
	}
	body := []byte(form.Encode())
	if len(body) > 65536 {
		return writeBranch("updateSubmission", UpdateSubmissionBranchDefect, sdkgo.FailureValidation, "encoded answers exceed the request limit")
	}
	return operation.client.invokeSafeWrite(call, "updateSubmission", http.MethodPost, "/submission/"+input.SubmissionID, body, "application/x-www-form-urlencoded", input.SubmissionID, safeWriteBranches{success: UpdateSubmissionBranchUpdated, notFound: UpdateSubmissionBranchNotFound, rejected: UpdateSubmissionBranchProviderRejected, invalid: UpdateSubmissionBranchInvalidResponse, defect: UpdateSubmissionBranchDefect})
}

// Invoke deletes one submission; repeated deletion has no second effect.
func (operation DeleteSubmissionOperation) Invoke(call sdkgo.Call, input GetSubmissionInput) sdkgo.MutationAttempt[WriteResult] {
	if !numericID.MatchString(input.SubmissionID) {
		return writeBranch("deleteSubmission", DeleteSubmissionBranchDefect, sdkgo.FailureValidation, "numeric submission ID is required")
	}
	return operation.client.invokeSafeWrite(call, "deleteSubmission", http.MethodDelete, "/submission/"+input.SubmissionID, nil, "", input.SubmissionID, safeWriteBranches{success: DeleteSubmissionBranchDeleted, notFound: DeleteSubmissionBranchNotFound, rejected: DeleteSubmissionBranchProviderRejected, invalid: DeleteSubmissionBranchInvalidResponse, defect: DeleteSubmissionBranchDefect})
}

type safeWriteBranches struct{ success, notFound, rejected, invalid, defect sdkgo.BranchID }

func (client *Client) invokeSafeWrite(call sdkgo.Call, operationName, method, path string, body []byte, contentType, id string, branches safeWriteBranches) sdkgo.MutationAttempt[WriteResult] {
	responseBody, status, retryAfter, err := client.requestWithContentType(call, method, path, body, contentType)
	if errors.Is(err, errCredential) {
		return writeBranch(operationName, branches.defect, sdkgo.FailureAuthentication, "Jotform credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) || (err != nil && !errors.Is(err, providerhttp.ErrBodyTooLarge)) {
		return sdkgo.NewMutationRetry[WriteResult](writeFailure(operationName, sdkgo.FailureTransport, "Jotform request or response was interrupted"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[WriteResult](writeFailure(operationName, sdkgo.FailureRateLimit, "Jotform rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[WriteResult](writeFailure(operationName, sdkgo.FailureAvailability, "Jotform is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return writeBranch(operationName, branches.notFound, sdkgo.FailureNotFound, "Jotform did not find the resource")
	}
	if status != http.StatusOK {
		return writeBranch(operationName, branches.rejected, sdkgo.FailureProviderRejection, "Jotform rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return writeBranch(operationName, branches.invalid, sdkgo.FailureResponseTooLarge, "Jotform response exceeded the configured limit")
	}
	var result struct {
		ResponseCode int `json:"responseCode"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil || result.ResponseCode == 0 {
		return writeBranch(operationName, branches.invalid, sdkgo.FailureProtocol, "Jotform returned invalid response JSON")
	}
	if result.ResponseCode == 429 || result.ResponseCode >= 500 {
		return sdkgo.NewMutationRetry[WriteResult](writeFailure(operationName, sdkgo.FailureAvailability, "Jotform reported temporary unavailability"), retryAfter)
	}
	if result.ResponseCode == 404 {
		return writeBranch(operationName, branches.notFound, sdkgo.FailureNotFound, "Jotform did not find the resource")
	}
	if result.ResponseCode != 200 {
		return writeBranch(operationName, branches.rejected, sdkgo.FailureProviderRejection, "Jotform rejected the request")
	}
	return sdkgo.NewMutationBranch(branches.success, WriteResult{ID: id, Accepted: true}, nil, sdkgo.Receipt{})
}

func writeFailure(operationName string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "jotform", Operation: operationName, Message: message}
}
func writeBranch(operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[WriteResult] {
	failure := writeFailure(operationName, kind, message)
	return sdkgo.NewMutationBranch(branch, WriteResult{}, &failure, sdkgo.Receipt{})
}
