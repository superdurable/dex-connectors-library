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
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// UserSummary contains a Lever user's role and activation state without contact details.
type UserSummary struct {
	// ID is the Lever user UID.
	ID string `json:"id"`
	// AccessRole is Lever's role name or custom role ID.
	AccessRole string `json:"accessRole"`
	// Active reports whether the user may log in.
	Active bool `json:"active"`
}

// UsersPage contains selected metadata for visible Lever users.
type UsersPage struct {
	// Users contains roles and activation states.
	Users []UserSummary `json:"users"`
}

// GetUserInput identifies one Lever user.
type GetUserInput struct {
	// ID is the Lever user UID.
	ID string `json:"id"`
}

// ListUsersInput filters Lever users.
type ListUsersInput struct {
	// Email exactly matches a Lever user's canonical email; blank includes all users.
	Email string `json:"email,omitempty"`
	// IncludeDeactivated includes inactive users when true.
	IncludeDeactivated bool `json:"includeDeactivated"`
}

// CreateUserInput supplies the required name, email, and explicit access role.
type CreateUserInput struct {
	// Name is the new user's preferred name.
	Name string `json:"name"`
	// Email is the new user's email address.
	Email string `json:"email"`
	// AccessRole is the Lever role name or custom role ID.
	AccessRole string `json:"accessRole"`
}

// UpdateUserInput replaces the supplied user profile fields.
type UpdateUserInput struct {
	// ID is the Lever user UID.
	ID string `json:"id"`
	// Name is the user's preferred name.
	Name string `json:"name"`
	// Email is the user's email address.
	Email string `json:"email"`
	// AccessRole is the Lever role name or custom role ID.
	AccessRole string `json:"accessRole"`
	// ExternalDirectoryID is an optional HR directory ID; blank clears it.
	ExternalDirectoryID string `json:"externalDirectoryId,omitempty"`
	// JobTitle is optional; blank clears it.
	JobTitle string `json:"jobTitle,omitempty"`
	// ManagerID is an optional Lever user UID; blank clears it.
	ManagerID string `json:"managerId,omitempty"`
	// LinkedContactIDs is the complete set of linked Lever contact UIDs.
	LinkedContactIDs []string `json:"linkedContactIds,omitempty"`
}

// SetUserActivationInput identifies one Lever user to deactivate or reactivate.
type SetUserActivationInput struct {
	// ID is the Lever user UID.
	ID string `json:"id"`
}

// GetUserOperation reads one user's role and activation state.
type GetUserOperation struct{ client *Client }

// GetUser binds the Query to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client} }

// Definition returns the generated Query contract.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Invoke reads user metadata without returning contact details.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[UserSummary] {
	if !validOpportunityID(input.ID) {
		return postingQueryBranch[UserSummary]("getUser", GetUserBranchDefect, sdkgo.FailureValidation, "user ID is invalid")
	}
	return readPostingQuery(operation.client.get(call, "/users/"+url.PathEscape(input.ID)), "getUser", GetUserBranchFound, GetUserBranchNotFound, GetUserBranchProviderRejected, GetUserBranchInvalidResponse, GetUserBranchDefect, decodeUser)
}

// ListUsersOperation lists selected user metadata.
type ListUsersOperation struct{ client *Client }

// ListUsers binds the Query to this client.
func (client *Client) ListUsers() ListUsersOperation { return ListUsersOperation{client} }

// Definition returns the generated Query contract.
func (ListUsersOperation) Definition() sdkgo.QueryDefinition { return ListUsersDefinition }

// Invoke lists user roles and activation states.
func (operation ListUsersOperation) Invoke(call sdkgo.Call, input ListUsersInput) sdkgo.QueryAttempt[UsersPage] {
	if input.Email != "" && !validEmail(input.Email) {
		return postingQueryBranch[UsersPage]("listUsers", ListUsersBranchDefect, sdkgo.FailureValidation, "email filter is invalid")
	}
	values := url.Values{}
	if input.Email != "" {
		values.Set("email", input.Email)
	}
	if input.IncludeDeactivated {
		values.Set("includeDeactivated", "true")
	}
	path := "/users"
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	return readPostingQuery(operation.client.get(call, path), "listUsers", ListUsersBranchListed, "", ListUsersBranchProviderRejected, ListUsersBranchInvalidResponse, ListUsersBranchDefect, decodeUsers)
}

// CreateUserOperation creates one Lever user.
type CreateUserOperation struct{ client *Client }

// CreateUser binds the Mutation to this client.
func (client *Client) CreateUser() CreateUserOperation { return CreateUserOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateUserOperation) Definition() sdkgo.MutationDefinition { return CreateUserDefinition }

// IdempotencyKey correlates repeats; Lever does not deduplicate user creates by this key.
func (CreateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one user with bounded sends and email read-back.
func (operation CreateUserOperation) Invoke(call sdkgo.Call, input CreateUserInput) sdkgo.MutationAttempt[UserSummary] {
	if !validCandidateName(input.Name) || !validEmail(input.Email) || strings.TrimSpace(input.AccessRole) == "" {
		return userMutationBranch("createUser", CreateUserBranchDefect, sdkgo.FailureValidation, "user name, email, or role is invalid", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever"}
	fence, err := singledispatch.NewFence(call, CreateUserDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return userMutationBranch("createUser", CreateUserBranchDefect, sdkgo.FailureLocalDefect, "user dispatch checkpoint failed", receipt)
	}
	decision := fence.Decide()
	if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
		readback := operation.client.get(call, "/users?"+url.Values{"email": {input.Email}, "includeDeactivated": {"true"}}.Encode())
		if readback.status == http.StatusOK && readback.err == nil {
			user, found, err := findCreatedUser(readback.body, input)
			if err == nil && found {
				receipt.ProviderObjectID = user.ID
				return sdkgo.NewMutationBranch(CreateUserBranchCreated, user, nil, receipt)
			}
			if err != nil {
				return sdkgo.NewMutationRetry[UserSummary](userFailure("createUser", sdkgo.FailureProtocol, "user read-back was invalid"), 0)
			}
		} else {
			return sdkgo.NewMutationRetry[UserSummary](userFailure("createUser", sdkgo.FailureAvailability, "user read-back unavailable"), readback.retryAfter)
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(UserSummary{}, userFailure("createUser", sdkgo.FailureTransport, "user may exist at the send cap"), receipt)
	}
	payload, err := json.Marshal(struct {
		Name       string `json:"name"`
		Email      string `json:"email"`
		AccessRole string `json:"accessRole"`
	}{input.Name, input.Email, input.AccessRole})
	if err != nil {
		return userMutationBranch("createUser", CreateUserBranchDefect, sdkgo.FailureLocalDefect, "user request encoding failed", receipt)
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doWithContext(call, requestContext, http.MethodPost, "/users", payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[UserSummary](userFailure("createUser", sdkgo.FailureAvailability, "user send claim was not durable"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[UserSummary](userFailure("createUser", sdkgo.FailureAvailability, "user retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[UserSummary](userFailure("createUser", kind, "Lever temporarily refused user creation"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(UserSummary{}, userFailure("createUser", sdkgo.FailureAuthentication, "credentials unavailable after a possible create"), receipt)
		}
		return userMutationBranch("createUser", CreateUserBranchDefect, sdkgo.FailureAuthentication, "Lever credentials unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return unknownUserCreate(call, decision.MaximumPriorSends, receipt, sdkgo.FailureTransport, "user create outcome is unknown")
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(UserSummary{}, userFailure("createUser", sdkgo.FailureProviderRejection, "Lever rejected a resend after a possible create"), receipt)
		}
		return userMutationBranch("createUser", CreateUserBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected user creation", receipt)
	}
	user, err := decodeUser(response.body)
	if err != nil || user.AccessRole != input.AccessRole {
		return unknownUserCreate(call, decision.MaximumPriorSends, receipt, sdkgo.FailureProtocol, "Lever did not confirm user creation")
	}
	if err := fence.RecordProviderID(user.ID); err != nil {
		return sdkgo.NewMutationRetry[UserSummary](userFailure("createUser", sdkgo.FailureAvailability, "user ID checkpoint failed"), 0)
	}
	receipt.ProviderObjectID = user.ID
	return sdkgo.NewMutationBranch(CreateUserBranchCreated, user, nil, receipt)
}

// UpdateUserOperation replaces one Lever user's profile fields.
type UpdateUserOperation struct{ client *Client }

// UpdateUser binds the Mutation to this client.
func (client *Client) UpdateUser() UpdateUserOperation { return UpdateUserOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateUserOperation) Definition() sdkgo.MutationDefinition { return UpdateUserDefinition }

// IdempotencyKey correlates absolute replacements.
func (UpdateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces user fields and checks the returned role.
func (operation UpdateUserOperation) Invoke(call sdkgo.Call, input UpdateUserInput) sdkgo.MutationAttempt[UserSummary] {
	if !validOpportunityID(input.ID) || !validCandidateName(input.Name) || !validEmail(input.Email) || strings.TrimSpace(input.AccessRole) == "" || input.ManagerID != "" && !validOpportunityID(input.ManagerID) {
		return userMutationBranch("updateUser", UpdateUserBranchDefect, sdkgo.FailureValidation, "user ID, name, email, role, or manager is invalid", sdkgo.Receipt{})
	}
	for _, id := range input.LinkedContactIDs {
		if !validOpportunityID(id) {
			return userMutationBranch("updateUser", UpdateUserBranchDefect, sdkgo.FailureValidation, "linked contact ID is invalid", sdkgo.Receipt{})
		}
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.ID}
	payload, err := json.Marshal(struct {
		Name                string   `json:"name"`
		Email               string   `json:"email"`
		AccessRole          string   `json:"accessRole"`
		ExternalDirectoryID string   `json:"externalDirectoryId,omitempty"`
		JobTitle            string   `json:"jobTitle,omitempty"`
		ManagerID           string   `json:"managerId,omitempty"`
		LinkedContactIDs    []string `json:"linkedContactIds,omitempty"`
	}{input.Name, input.Email, input.AccessRole, input.ExternalDirectoryID, input.JobTitle, input.ManagerID, input.LinkedContactIDs})
	if err != nil {
		return userMutationBranch("updateUser", UpdateUserBranchDefect, sdkgo.FailureLocalDefect, "user request encoding failed", receipt)
	}
	response := operation.client.do(call, http.MethodPut, "/users/"+url.PathEscape(input.ID), payload)
	if errors.Is(response.err, errCredential) {
		return userMutationBranch("updateUser", UpdateUserBranchDefect, sdkgo.FailureAuthentication, "Lever credentials unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.err != nil {
		return sdkgo.NewMutationRetry[UserSummary](userFailure("updateUser", sdkgo.FailureTransport, "user replacement outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[UserSummary](userFailure("updateUser", sdkgo.FailureRateLimit, "Lever rate limited user replacement"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[UserSummary](userFailure("updateUser", sdkgo.FailureAvailability, "user replacement outcome is unknown"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return userMutationBranch("updateUser", UpdateUserBranchNotFound, sdkgo.FailureNotFound, "Lever did not find the user", receipt)
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return userMutationBranch("updateUser", UpdateUserBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected user replacement", receipt)
	}
	user, err := decodeUser(response.body)
	if err != nil || user.ID != input.ID || user.AccessRole != input.AccessRole {
		return sdkgo.NewMutationRetry[UserSummary](userFailure("updateUser", sdkgo.FailureProtocol, "Lever did not confirm user replacement"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateUserBranchUpdated, user, nil, receipt)
}

// DeactivateUserOperation disables one Lever user's login.
type DeactivateUserOperation struct{ client *Client }

// DeactivateUser binds the Mutation to this client.
func (client *Client) DeactivateUser() DeactivateUserOperation {
	return DeactivateUserOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeactivateUserOperation) Definition() sdkgo.MutationDefinition { return DeactivateUserDefinition }

// IdempotencyKey correlates retries of an absolute activation change.
func (DeactivateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ SetUserActivationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deactivates a user and confirms the resulting state.
func (operation DeactivateUserOperation) Invoke(call sdkgo.Call, input SetUserActivationInput) sdkgo.MutationAttempt[UserSummary] {
	return operation.client.setUserActivation(call, input, false)
}

// ReactivateUserOperation restores one Lever user's login.
type ReactivateUserOperation struct{ client *Client }

// ReactivateUser binds the Mutation to this client.
func (client *Client) ReactivateUser() ReactivateUserOperation {
	return ReactivateUserOperation{client}
}

// Definition returns the generated Mutation contract.
func (ReactivateUserOperation) Definition() sdkgo.MutationDefinition { return ReactivateUserDefinition }

// IdempotencyKey correlates retries of an absolute activation change.
func (ReactivateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ SetUserActivationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reactivates a user and confirms the resulting state.
func (operation ReactivateUserOperation) Invoke(call sdkgo.Call, input SetUserActivationInput) sdkgo.MutationAttempt[UserSummary] {
	return operation.client.setUserActivation(call, input, true)
}

func (client *Client) setUserActivation(call sdkgo.Call, input SetUserActivationInput, active bool) sdkgo.MutationAttempt[UserSummary] {
	operation := "deactivateUser"
	success, notFound, rejected, defect := DeactivateUserBranchUpdated, DeactivateUserBranchNotFound, DeactivateUserBranchProviderRejected, DeactivateUserBranchDefect
	action := "deactivate"
	if active {
		operation, success, notFound, rejected, defect, action = "reactivateUser", ReactivateUserBranchUpdated, ReactivateUserBranchNotFound, ReactivateUserBranchProviderRejected, ReactivateUserBranchDefect, "reactivate"
	}
	if !validOpportunityID(input.ID) {
		return userMutationBranch(operation, defect, sdkgo.FailureValidation, "user ID is invalid", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.ID}
	response := client.do(call, http.MethodPost, "/users/"+url.PathEscape(input.ID)+"/"+action, nil)
	if errors.Is(response.err, errCredential) {
		return userMutationBranch(operation, defect, sdkgo.FailureAuthentication, "Lever credentials unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.err != nil {
		return sdkgo.NewMutationRetry[UserSummary](userFailure(operation, sdkgo.FailureTransport, "user activation outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[UserSummary](userFailure(operation, sdkgo.FailureRateLimit, "Lever rate limited user activation"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[UserSummary](userFailure(operation, sdkgo.FailureAvailability, "user activation outcome is unknown"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return userMutationBranch(operation, notFound, sdkgo.FailureNotFound, "Lever did not find the user", receipt)
	}
	if response.status != http.StatusOK {
		if response.status == http.StatusBadRequest || response.status == http.StatusConflict {
			readback := client.get(call, "/users/"+url.PathEscape(input.ID))
			if readback.status == http.StatusOK && readback.err == nil {
				user, err := decodeUser(readback.body)
				if err == nil && user.Active == active {
					return sdkgo.NewMutationBranch(success, user, nil, receipt)
				}
			}
		}
		return userMutationBranch(operation, rejected, sdkgo.FailureProviderRejection, "Lever rejected user activation", receipt)
	}
	user, err := decodeUser(response.body)
	if err != nil || user.ID != input.ID || user.Active != active {
		return sdkgo.NewMutationRetry[UserSummary](userFailure(operation, sdkgo.FailureProtocol, "Lever did not confirm user activation"), 0)
	}
	return sdkgo.NewMutationBranch(success, user, nil, receipt)
}

func decodeUser(body []byte) (UserSummary, error) {
	var wire struct {
		Data struct {
			ID            string `json:"id"`
			AccessRole    string `json:"accessRole"`
			DeactivatedAt *int64 `json:"deactivatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || !validOpportunityID(wire.Data.ID) || strings.TrimSpace(wire.Data.AccessRole) == "" {
		return UserSummary{}, fmt.Errorf("Lever user JSON invalid")
	}
	return UserSummary{ID: wire.Data.ID, AccessRole: wire.Data.AccessRole, Active: wire.Data.DeactivatedAt == nil}, nil
}
func decodeUsers(body []byte) (UsersPage, error) {
	var wire struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil {
		return UsersPage{}, fmt.Errorf("Lever users JSON invalid")
	}
	page := UsersPage{Users: make([]UserSummary, 0, len(wire.Data))}
	for _, entry := range wire.Data {
		user, err := decodeUser(append([]byte(`{"data":`), append(entry, '}')...))
		if err != nil {
			return UsersPage{}, err
		}
		page.Users = append(page.Users, user)
	}
	return page, nil
}
func findCreatedUser(body []byte, input CreateUserInput) (UserSummary, bool, error) {
	var wire struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Email         string `json:"email"`
			AccessRole    string `json:"accessRole"`
			DeactivatedAt *int64 `json:"deactivatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil {
		return UserSummary{}, false, fmt.Errorf("Lever user lookup JSON invalid")
	}
	for _, entry := range wire.Data {
		if entry.Email == input.Email && entry.Name == input.Name && entry.AccessRole == input.AccessRole && validOpportunityID(entry.ID) {
			return UserSummary{ID: entry.ID, AccessRole: entry.AccessRole, Active: entry.DeactivatedAt == nil}, true, nil
		}
	}
	return UserSummary{}, false, nil
}
func unknownUserCreate(call sdkgo.Call, priorSends int32, receipt sdkgo.Receipt, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UserSummary] {
	if priorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[UserSummary](userFailure("createUser", kind, message), 0)
	}
	return sdkgo.NewMutationUncertain(UserSummary{}, userFailure("createUser", kind, message+" at the send cap"), receipt)
}
func userFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: operation, Message: message}
}
func userMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UserSummary] {
	failure := userFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, UserSummary{}, &failure, receipt)
}
