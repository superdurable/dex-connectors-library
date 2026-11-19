// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListUsersInput selects one cursor-paginated page of Harvest users.
type ListUsersInput struct {
	// Cursor is the opaque cursor from the previous page; blank starts a listing.
	Cursor string `json:"cursor,omitempty"`
	// PerPage is the page size, from 1 through 2000; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
}

// GetUserInput identifies one Harvest user.
type GetUserInput struct {
	// ID is the positive Harvest user ID.
	ID int64 `json:"id"`
}

// GetCurrentUserInput selects the user who owns the connection.
type GetCurrentUserInput struct{}

// CreateUserInput invites a teammate to the Harvest account.
type CreateUserInput struct {
	// FirstName is the invited user's first name.
	FirstName string `json:"firstName"`
	// LastName is the invited user's last name.
	LastName string `json:"lastName"`
	// Email is the invited user's email address.
	Email string `json:"email"`
	// IsContractor optionally marks the teammate as a contractor.
	IsContractor *bool `json:"isContractor,omitempty"`
}

// UpdateUserInput sets absolute user details.
type UpdateUserInput struct {
	// ID is the positive Harvest user ID.
	ID int64 `json:"id"`
	// FirstName replaces the first name when supplied.
	FirstName *string `json:"firstName,omitempty"`
	// LastName replaces the last name when supplied.
	LastName *string `json:"lastName,omitempty"`
	// IsActive archives or restores the user when supplied.
	IsActive *bool `json:"isActive,omitempty"`
	// IsContractor sets contractor status when supplied.
	IsContractor *bool `json:"isContractor,omitempty"`
}

// DeleteUserInput identifies one user for deletion.
type DeleteUserInput struct {
	// ID is the positive Harvest user ID.
	ID int64 `json:"id"`
}

// UserRecord is a Harvest teammate's identity and active state.
type UserRecord struct {
	// ID is assigned by Harvest.
	ID int64 `json:"id"`
	// FirstName is the user's first name.
	FirstName string `json:"firstName"`
	// LastName is the user's last name.
	LastName string `json:"lastName"`
	// Email is the user's account email address.
	Email string `json:"email"`
	// IsActive reports whether the user is active.
	IsActive bool `json:"isActive"`
}

// UserPage contains one page of users and its next cursor.
type UserPage struct {
	// Users are returned in Harvest's creation-date order.
	Users []UserRecord `json:"users"`
	// NextCursor is blank on the final page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// DeletedUser identifies a user whose deletion Harvest confirmed.
type DeletedUser struct {
	// ID is the deleted Harvest user ID.
	ID int64 `json:"id"`
}

// ListUsersOperation reads a page of Harvest users.
type ListUsersOperation struct{ client *Client }

// ListUsers binds the user-list Query to this client.
func (client *Client) ListUsers() ListUsersOperation { return ListUsersOperation{client} }

// Definition returns the generated user-list contract.
func (ListUsersOperation) Definition() sdkgo.QueryDefinition { return ListUsersDefinition }

// Invoke validates pagination and reads one user page.
func (operation ListUsersOperation) Invoke(call sdkgo.Call, input ListUsersInput) sdkgo.QueryAttempt[UserPage] {
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[UserPage]("listUsers", ListUsersBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return invokeGet(operation.client, call, "listUsers", harvestPagePath("/users", input.Cursor, input.PerPage), ListUsersBranchListed, ListUsersBranchProviderRejected, decodeUserPage)
}

// GetUserOperation reads one Harvest user.
type GetUserOperation struct{ client *Client }

// GetUser binds the user lookup Query to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client} }

// Definition returns the generated user lookup contract.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Invoke validates the ID and reads a user.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[UserRecord] {
	if input.ID <= 0 {
		return failedBranch[UserRecord]("getUser", GetUserBranchDefect, sdkgo.FailureValidation, "user ID must be positive")
	}
	return invokeGet(operation.client, call, "getUser", "/users/"+strconv.FormatInt(input.ID, 10), GetUserBranchFound, GetUserBranchNotFound, decodeUserRecord)
}

// GetCurrentUserOperation reads the connection owner's Harvest user.
type GetCurrentUserOperation struct{ client *Client }

// GetCurrentUser binds the current-user Query to this client.
func (client *Client) GetCurrentUser() GetCurrentUserOperation {
	return GetCurrentUserOperation{client}
}

// Definition returns the generated current-user contract.
func (GetCurrentUserOperation) Definition() sdkgo.QueryDefinition { return GetCurrentUserDefinition }

// Invoke reads the authenticated user without a user ID.
func (operation GetCurrentUserOperation) Invoke(call sdkgo.Call, _ GetCurrentUserInput) sdkgo.QueryAttempt[UserRecord] {
	return invokeGet(operation.client, call, "getCurrentUser", "/users/me", GetCurrentUserBranchFound, GetCurrentUserBranchProviderRejected, decodeUserRecord)
}

// CreateUserOperation invites one teammate behind a bounded dispatch fence.
type CreateUserOperation struct{ client *Client }

// CreateUser binds the invitation Mutation to this client.
func (client *Client) CreateUser() CreateUserOperation { return CreateUserOperation{client} }

// Definition returns the generated user creation contract.
func (CreateUserOperation) Definition() sdkgo.MutationDefinition { return CreateUserDefinition }

// IdempotencyKey records the call identity; Harvest does not deduplicate invitations.
func (CreateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends an invitation under the limited-repeat fence.
func (operation CreateUserOperation) Invoke(call sdkgo.Call, input CreateUserInput) sdkgo.MutationAttempt[UserRecord] {
	address, err := mail.ParseAddress(input.Email)
	if strings.TrimSpace(input.FirstName) == "" || strings.TrimSpace(input.LastName) == "" || len(input.FirstName) > 255 || len(input.LastName) > 255 || err != nil || address.Address != input.Email || len(input.Email) > 320 {
		return harvestMutationFailure[UserRecord]("createUser", CreateUserBranchDefect, sdkgo.FailureValidation, "user name or email is invalid")
	}
	payload, err := json.Marshal(struct {
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		Email        string `json:"email"`
		IsContractor *bool  `json:"is_contractor,omitempty"`
	}{input.FirstName, input.LastName, input.Email, input.IsContractor})
	if err != nil {
		return harvestMutationFailure[UserRecord]("createUser", CreateUserBranchDefect, sdkgo.FailureValidation, "user input cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createUser", CreateUserDefinition.StepDefaults.ExecuteMethodTimeout, "/users", payload, CreateUserBranchCreated, CreateUserBranchProviderRejected, CreateUserBranchDefect, decodeUserRecord, func(user UserRecord) string { return strconv.FormatInt(user.ID, 10) })
}

// UpdateUserOperation changes absolute teammate fields.
type UpdateUserOperation struct{ client *Client }

// UpdateUser binds the user update Mutation to this client.
func (client *Client) UpdateUser() UpdateUserOperation { return UpdateUserOperation{client} }

// Definition returns the generated user update contract.
func (UpdateUserOperation) Definition() sdkgo.MutationDefinition { return UpdateUserDefinition }

// IdempotencyKey records the stable Dex call identity for this update.
func (UpdateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies an idempotent PATCH to the user.
func (operation UpdateUserOperation) Invoke(call sdkgo.Call, input UpdateUserInput) sdkgo.MutationAttempt[UserRecord] {
	if input.ID <= 0 || (input.FirstName == nil && input.LastName == nil && input.IsActive == nil && input.IsContractor == nil) ||
		(input.FirstName != nil && (strings.TrimSpace(*input.FirstName) == "" || len(*input.FirstName) > 255)) ||
		(input.LastName != nil && (strings.TrimSpace(*input.LastName) == "" || len(*input.LastName) > 255)) {
		return harvestMutationFailure[UserRecord]("updateUser", UpdateUserBranchDefect, sdkgo.FailureValidation, "user ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		FirstName    *string `json:"first_name,omitempty"`
		LastName     *string `json:"last_name,omitempty"`
		IsActive     *bool   `json:"is_active,omitempty"`
		IsContractor *bool   `json:"is_contractor,omitempty"`
	}{input.FirstName, input.LastName, input.IsActive, input.IsContractor})
	if err != nil {
		return harvestMutationFailure[UserRecord]("updateUser", UpdateUserBranchDefect, sdkgo.FailureValidation, "user update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateUser", http.MethodPatch, "/users/"+strconv.FormatInt(input.ID, 10), payload, UpdateUserBranchUpdated, UpdateUserBranchNotFound, UpdateUserBranchProviderRejected, decodeUserRecord)
}

// DeleteUserOperation deletes a teammate without tracked time or expenses.
type DeleteUserOperation struct{ client *Client }

// DeleteUser binds the user deletion Mutation to this client.
func (client *Client) DeleteUser() DeleteUserOperation { return DeleteUserOperation{client} }

// Definition returns the generated user deletion contract.
func (DeleteUserOperation) Definition() sdkgo.MutationDefinition { return DeleteUserDefinition }

// IdempotencyKey records the stable Dex call identity for this deletion.
func (DeleteUserOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the user with a safely repeatable DELETE.
func (operation DeleteUserOperation) Invoke(call sdkgo.Call, input DeleteUserInput) sdkgo.MutationAttempt[DeletedUser] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedUser]("deleteUser", DeleteUserBranchDefect, sdkgo.FailureValidation, "user ID must be positive")
	}
	decode := func(_ []byte) (DeletedUser, error) { return DeletedUser{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteUser", http.MethodDelete, "/users/"+strconv.FormatInt(input.ID, 10), nil, DeleteUserBranchDeleted, DeleteUserBranchNotFound, DeleteUserBranchProviderRejected, decode)
}

func decodeUserPage(body []byte) (UserPage, error) {
	var wire struct {
		Users []userWire `json:"users"`
		Links struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return UserPage{}, err
	}
	if wire.Users == nil {
		return UserPage{}, fmt.Errorf("missing users")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/users")
	if err != nil {
		return UserPage{}, err
	}
	page := UserPage{Users: make([]UserRecord, 0, len(wire.Users)), NextCursor: next}
	for _, user := range wire.Users {
		if user.ID <= 0 {
			return UserPage{}, fmt.Errorf("invalid user identity")
		}
		page.Users = append(page.Users, user.record())
	}
	return page, nil
}

func decodeUserRecord(body []byte) (UserRecord, error) {
	var wire userWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return UserRecord{}, err
	}
	if wire.ID <= 0 || wire.FirstName == "" || wire.LastName == "" || wire.Email == "" {
		return UserRecord{}, fmt.Errorf("missing user identity")
	}
	return wire.record(), nil
}

type userWire struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	IsActive  bool   `json:"is_active"`
}

func (wire userWire) record() UserRecord {
	return UserRecord{ID: wire.ID, FirstName: wire.FirstName, LastName: wire.LastName, Email: wire.Email, IsActive: wire.IsActive}
}
