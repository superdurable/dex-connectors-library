// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	findUserByEmailOperationID = "findUserByEmail"

	findUserByEmailDocument = `query LinearFindUserByEmail($email: String!, $includeDisabled: Boolean) {
  users(filter: { email: { eqIgnoreCase: $email } }, first: 2, includeDisabled: $includeDisabled) {
    nodes { id name displayName email active admin guest url }
  }
}`
)

// FindUserByEmailInput names the email address to resolve.
type FindUserByEmailInput struct {
	// Email is the user's address, such as alice@example.com, compared without case.
	Email string `json:"email"`
	// IncludesDisabled also finds a suspended or deactivated user; false finds only active users.
	IncludesDisabled bool `json:"includesDisabled,omitempty"`
}

// User is one workspace member.
type User struct {
	// ID is the user's UUID, which createIssue and updateIssue accept as AssigneeID.
	ID string `json:"id"`
	// Name is the user's full name.
	Name string `json:"name"`
	// DisplayName is the user's short display name.
	DisplayName string `json:"displayName"`
	// Email is the user's email address.
	Email string `json:"email"`
	// IsActive reports that the user is not suspended or deactivated.
	IsActive bool `json:"active"`
	// IsAdmin reports a workspace admin.
	IsAdmin bool `json:"admin,omitempty"`
	// IsGuest reports a guest limited to some teams.
	IsGuest bool `json:"guest,omitempty"`
	// URL is the user's profile in Linear.
	URL string `json:"url,omitempty"`
}

// FindUserByEmailOperation implements the findUserByEmail Query.
type FindUserByEmailOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (FindUserByEmailOperation) Definition() sdkgo.QueryDefinition { return FindUserByEmailDefinition }

// Invoke finds the user. Linear keeps one account per address in a workspace, so a second match is an
// invalid answer rather than a choice.
func (operation FindUserByEmailOperation) Invoke(call sdkgo.Call, input FindUserByEmailInput) sdkgo.QueryAttempt[User] {
	email, err := validateEmail(input.Email)
	if err != nil {
		return sdkgo.NewQueryBranch(FindUserByEmailBranchDefect, User{}, linearFailurePointer(sdkgo.FailureValidation, findUserByEmailOperationID, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failed := operation.client.openSession(call, findUserByEmailOperationID)
	defer cancel()
	if failed != nil {
		return queryAttemptForExchange(*failed, User{}, sdkgo.Receipt{CallID: call.ID, Provider: providerName}, findUserByEmailBranches)
	}
	result := session.exchange(graphQLRequest{operationName: "LinearFindUserByEmail", document: findUserByEmailDocument,
		variables: map[string]any{"email": email, "includeDisabled": input.IncludesDisabled}})
	receipt := session.receipt(result.response, "")
	if result.outcome != exchangeSucceeded {
		return queryAttemptForExchange(result, User{}, receipt, findUserByEmailBranches)
	}
	users, err := decodeUsers(result.response.data, email)
	switch {
	case err != nil:
		return sdkgo.NewQueryBranch(FindUserByEmailBranchInvalidResponse, User{}, linearFailurePointer(sdkgo.FailureProtocol, findUserByEmailOperationID, err.Error()), receipt)
	case len(users) == 0:
		return sdkgo.NewQueryBranch(FindUserByEmailBranchNotFound, User{}, linearFailurePointer(sdkgo.FailureNotFound, findUserByEmailOperationID,
			"no Linear user that the connection can see has this email"), receipt)
	}
	receipt.ProviderObjectID = users[0].ID
	return sdkgo.NewQueryBranch(FindUserByEmailBranchFound, users[0], nil, receipt)
}

var findUserByEmailBranches = queryBranches{
	providerRejected: FindUserByEmailBranchProviderRejected, invalidResponse: FindUserByEmailBranchInvalidResponse, defect: FindUserByEmailBranchDefect,
}

// decodeUsers requires every returned user to carry the requested address.
func decodeUsers(data json.RawMessage, email string) ([]User, error) {
	var document struct {
		Users *struct {
			Nodes []User `json:"nodes"`
		} `json:"users"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Users == nil {
		return nil, errors.New("Linear returned a malformed user list")
	}
	if len(document.Users.Nodes) > 1 {
		return nil, errors.New("Linear returned more than one user for one email address")
	}
	for _, user := range document.Users.Nodes {
		if !isLinearUUID(user.ID) || !strings.EqualFold(strings.TrimSpace(user.Email), email) {
			return nil, errors.New("Linear returned a malformed user or a user with another email")
		}
	}
	return document.Users.Nodes, nil
}
