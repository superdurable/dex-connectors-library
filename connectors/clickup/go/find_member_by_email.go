// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const findMemberByEmailOperation = "findMemberByEmail"

// MemberRole is ClickUp's Workspace role number.
type MemberRole int

const (
	// MemberRoleOwner is the Workspace owner, role 1.
	MemberRoleOwner MemberRole = 1
	// MemberRoleAdmin is an admin, role 2.
	MemberRoleAdmin MemberRole = 2
	// MemberRoleMember is a member, role 3.
	MemberRoleMember MemberRole = 3
	// MemberRoleGuest is a guest, role 4.
	MemberRoleGuest MemberRole = 4
)

// FindMemberByEmailInput names one Workspace and one email address.
type FindMemberByEmailInput struct {
	// WorkspaceID is the Workspace (ClickUp API team) ID, the first number in a ClickUp web address.
	WorkspaceID string `json:"workspaceId"`
	// Email is one bare email address, compared without regard to case.
	Email string `json:"email"`
}

// Member is one member of a Workspace.
type Member struct {
	// ID is ClickUp's numeric user ID, the value task assignees use.
	ID int64 `json:"id"`
	// Username is the member's display name.
	Username string `json:"username,omitempty"`
	// Email is the member's email address as ClickUp stores it.
	Email string `json:"email"`
	// Role is 1 owner, 2 admin, 3 member, or 4 guest, or zero when ClickUp does not report it.
	Role MemberRole `json:"role,omitempty"`
	// WorkspaceID is the Workspace the member belongs to.
	WorkspaceID string `json:"workspaceId"`
}

// FindMemberByEmailOperation is the findMemberByEmail Query.
type FindMemberByEmailOperation struct {
	client *Client
}

type authorizedWorkspacesWire struct {
	Teams []struct {
		ID      flexibleString `json:"id"`
		Members []struct {
			User struct {
				ID       flexibleInt64 `json:"id"`
				Username string        `json:"username"`
				Email    string        `json:"email"`
				Role     int           `json:"role"`
			} `json:"user"`
		} `json:"members"`
	} `json:"teams"`
}

// Definition returns the immutable connector operation definition.
func (FindMemberByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindMemberByEmailDefinition
}

// Invoke reads GET /team, the Workspaces the token's user belongs to with their members, and returns
// the member of WorkspaceID whose email equals Email.
func (operation FindMemberByEmailOperation) Invoke(call sdkgo.Call, input FindMemberByEmailInput) sdkgo.QueryAttempt[Member] {
	if err := validateFindMemberByEmailInput(input); err != nil {
		return sdkgo.NewQueryBranch(FindMemberByEmailBranchDefect, Member{}, clickupFailurePointer(sdkgo.FailureValidation, findMemberByEmailOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, findMemberByEmailOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(FindMemberByEmailBranchDefect, Member{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call.Context, credentials, findMemberByEmailOperation, clickupRequest{method: http.MethodGet, path: "/team"})
	receipt := operation.client.receipt(call, input.WorkspaceID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewQueryRetry[Member](result.failure, result.retryAfter)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(FindMemberByEmailBranchInvalidResponse, Member{}, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(FindMemberByEmailBranchDefect, Member{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(FindMemberByEmailBranchProviderRejected, Member{}, &result.failure, receipt)
	}
	var workspaces authorizedWorkspacesWire
	if err := json.Unmarshal(result.response.body, &workspaces); err != nil || workspaces.Teams == nil {
		return sdkgo.NewQueryBranch(FindMemberByEmailBranchInvalidResponse, Member{}, clickupFailurePointer(sdkgo.FailureProtocol, findMemberByEmailOperation,
			"ClickUp returned an invalid list of Workspaces"), receipt)
	}
	for _, workspace := range workspaces.Teams {
		if string(workspace.ID) != input.WorkspaceID {
			continue
		}
		for _, member := range workspace.Members {
			if !strings.EqualFold(strings.TrimSpace(member.User.Email), input.Email) {
				continue
			}
			if member.User.ID <= 0 {
				return sdkgo.NewQueryBranch(FindMemberByEmailBranchInvalidResponse, Member{}, clickupFailurePointer(sdkgo.FailureProtocol, findMemberByEmailOperation,
					"ClickUp returned a member without a valid user ID"), receipt)
			}
			receipt.ProviderObjectID = strconv.FormatInt(int64(member.User.ID), 10)
			return sdkgo.NewQueryBranch(FindMemberByEmailBranchFound, Member{
				ID: int64(member.User.ID), Username: member.User.Username, Email: strings.TrimSpace(member.User.Email),
				Role: MemberRole(member.User.Role), WorkspaceID: input.WorkspaceID,
			}, nil, receipt)
		}
		return sdkgo.NewQueryBranch(FindMemberByEmailBranchNotFound, Member{}, clickupFailurePointer(sdkgo.FailureNotFound, findMemberByEmailOperation,
			"no member of the Workspace has the email address"), receipt)
	}
	return sdkgo.NewQueryBranch(FindMemberByEmailBranchNotFound, Member{}, clickupFailurePointer(sdkgo.FailureNotFound, findMemberByEmailOperation,
		"the token's user is not a member of the Workspace"), receipt)
}

func validateFindMemberByEmailInput(input FindMemberByEmailInput) error {
	if err := validateNumericID("workspaceId", input.WorkspaceID); err != nil {
		return err
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || address.Name != "" {
		return errors.New("email must be one bare email address, such as ada@example.com")
	}
	return nil
}
