// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iterable

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

// UpsertUserInput identifies an Iterable user and supplies non-secret profile fields.
type UpsertUserInput struct {
	// Email identifies an email-based or hybrid project user; use either Email or UserID.
	Email string `json:"email,omitempty"`
	// UserID identifies a user ID-based project user; use either Email or UserID.
	UserID string `json:"userId,omitempty"`
	// DataFields contains non-secret profile properties to create or update.
	DataFields map[string]any `json:"dataFields,omitempty"`
}

// DeleteUserInput identifies a user by exactly one of email or user ID.
type DeleteUserInput struct {
	// Email is the email of a user in an email-based or hybrid project.
	Email string `json:"email,omitempty"`
	// UserID is the stable ID of a user in a user ID-based or hybrid project.
	UserID string `json:"userId,omitempty"`
}

// UserWriteResult records Iterable's acceptance without exposing profile data.
type UserWriteResult struct {
	// Accepted is true when Iterable accepted the write.
	Accepted bool `json:"accepted"`
}

// UpsertUserOperation creates or updates a user by stable identity.
type UpsertUserOperation struct{ client *Client }

// DeleteUserOperation deletes a user by stable identity.
type DeleteUserOperation struct{ client *Client }

// UpsertUser binds this mutation to the client.
func (client *Client) UpsertUser() UpsertUserOperation { return UpsertUserOperation{client} }

// DeleteUser binds this mutation to the client.
func (client *Client) DeleteUser() DeleteUserOperation { return DeleteUserOperation{client} }

// Definition returns the manifest-generated mutation contract.
func (UpsertUserOperation) Definition() sdkgo.MutationDefinition { return UpsertUserDefinition }

// Definition returns the manifest-generated mutation contract.
func (DeleteUserOperation) Definition() sdkgo.MutationDefinition { return DeleteUserDefinition }

// IdempotencyKey identifies one stable user upsert attempt across Dex retries.
func (UpsertUserOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies one stable user deletion across Dex retries.
func (DeleteUserOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends an absolute user-profile update, safe to repeat with identical input.
func (operation UpsertUserOperation) Invoke(call sdkgo.Call, input UpsertUserInput) sdkgo.MutationAttempt[UserWriteResult] {
	if !validUserSelection(input.Email, input.UserID) {
		return userWriteBranch("upsertUser", UpsertUserBranchDefect, sdkgo.FailureValidation, "one valid user identity is required")
	}
	requestBody, err := json.Marshal(input)
	if err != nil || len(requestBody) > 65536 {
		return userWriteBranch("upsertUser", UpsertUserBranchDefect, sdkgo.FailureValidation, "user profile fields must form bounded JSON")
	}
	response := operation.client.request(call, http.MethodPost, "/users/update", requestBody)
	return classifyUserWrite(response, "upsertUser", UpsertUserBranchUpdated, "", UpsertUserBranchProviderRejected, UpsertUserBranchInvalidResponse, UpsertUserBranchDefect)
}

// Invoke deletes a selected user; repeating deletion cannot create another effect.
func (operation DeleteUserOperation) Invoke(call sdkgo.Call, input DeleteUserInput) sdkgo.MutationAttempt[UserWriteResult] {
	if !validUserSelection(input.Email, input.UserID) {
		return userWriteBranch("deleteUser", DeleteUserBranchDefect, sdkgo.FailureValidation, "one valid user email or ID is required")
	}
	path := "/users/" + url.PathEscape(input.Email)
	if input.UserID != "" {
		path = "/users/byUserId/" + url.PathEscape(input.UserID)
	}
	response := operation.client.request(call, http.MethodDelete, path, nil)
	return classifyUserWrite(response, "deleteUser", DeleteUserBranchDeleted, DeleteUserBranchNotFound, DeleteUserBranchProviderRejected, DeleteUserBranchInvalidResponse, DeleteUserBranchDefect)
}

func validUserSelection(email, userID string) bool {
	if (email == "") == (userID == "") {
		return false
	}
	if email != "" {
		return validEmail(email)
	}
	return validUserID(userID)
}

func validUserID(userID string) bool {
	if len(userID) == 0 || len(userID) > 256 || strings.TrimSpace(userID) != userID {
		return false
	}
	for _, character := range userID {
		if character < 32 || character == 127 || character == '/' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

func classifyUserWrite(response providerResponse, operationName string, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[UserWriteResult] {
	if errors.Is(response.err, errCredential) {
		return userWriteBranch(operationName, defectBranch, sdkgo.FailureAuthentication, "Iterable credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) || (response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge)) {
		return sdkgo.NewMutationRetry[UserWriteResult](userWriteFailure(operationName, sdkgo.FailureTransport, "Iterable request or response was interrupted"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[UserWriteResult](userWriteFailure(operationName, sdkgo.FailureRateLimit, "Iterable rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[UserWriteResult](userWriteFailure(operationName, sdkgo.FailureAvailability, "Iterable is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound && notFoundBranch != "" {
		return userWriteBranch(operationName, notFoundBranch, sdkgo.FailureNotFound, "Iterable did not find the user")
	}
	if response.status != http.StatusOK {
		return userWriteBranch(operationName, rejectedBranch, sdkgo.FailureProviderRejection, "Iterable rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return userWriteBranch(operationName, invalidBranch, sdkgo.FailureResponseTooLarge, "Iterable response exceeded the configured limit")
	}
	var result struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.body, &result); err != nil || result.Code == "" {
		return userWriteBranch(operationName, invalidBranch, sdkgo.FailureProtocol, "Iterable returned invalid response JSON")
	}
	if result.Code != "Success" {
		return userWriteBranch(operationName, rejectedBranch, sdkgo.FailureProviderRejection, "Iterable rejected the user write")
	}
	return sdkgo.NewMutationBranch(successBranch, UserWriteResult{Accepted: true}, nil, sdkgo.Receipt{})
}

func userWriteFailure(operationName string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "iterable", Operation: operationName, Message: message}
}

func userWriteBranch(operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UserWriteResult] {
	failure := userWriteFailure(operationName, kind, message)
	return sdkgo.NewMutationBranch(branch, UserWriteResult{}, &failure, sdkgo.Receipt{})
}
