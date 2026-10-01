// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GroupRole is a member's role in a Google group.
type GroupRole string

const (
	// GroupRoleMember can subscribe to the group, read its archive, and see its membership.
	GroupRoleMember GroupRole = "MEMBER"
	// GroupRoleManager can do what an owner can except make owners or delete the group; Google offers it only when Groups for Business is on.
	GroupRoleManager GroupRole = "MANAGER"
	// GroupRoleOwner can change members, roles, and settings and delete the group.
	GroupRoleOwner GroupRole = "OWNER"
)

// AddUserToGroupInput adds one account to one group.
type AddUserToGroupInput struct {
	// GroupKey is the group's email address, an alias, or its unique group ID.
	GroupKey string `json:"groupKey"`
	// MemberEmail is the account's primary or alias email address.
	MemberEmail string `json:"memberEmail"`
	// Role is MEMBER, MANAGER, or OWNER; blank adds the account as MEMBER.
	Role GroupRole `json:"role,omitempty"`
}

// AddUserToGroupOutput is the account's direct membership.
type AddUserToGroupOutput struct {
	// Membership is the membership Google returned or read back.
	Membership GroupMembership `json:"membership"`
	// WasAlreadyMember reports that the account was already a direct member; its existing role is unchanged.
	WasAlreadyMember bool `json:"wasAlreadyMember"`
}

// GroupMembership is one direct member of a group.
type GroupMembership struct {
	// GroupKey is the group key the Step requested.
	GroupKey string `json:"groupKey"`
	// MemberID is the member's unique ID; for an account it is the user ID.
	MemberID string `json:"memberId"`
	// Email is the member's address as Google reports it.
	Email string `json:"email,omitempty"`
	// Role is the member's role, which may differ from the requested role when WasAlreadyMember is true.
	Role GroupRole `json:"role"`
	// Type is USER, GROUP, CUSTOMER, or EXTERNAL.
	Type string `json:"type,omitempty"`
	// Status is Google's membership status, such as ACTIVE, when reported.
	Status string `json:"status,omitempty"`
}

// RemoveUserFromGroupInput removes one account's direct membership from one group.
type RemoveUserFromGroupInput struct {
	// GroupKey is the group's email address, an alias, or its unique group ID.
	GroupKey string `json:"groupKey"`
	// MemberKey is the account's primary or alias email address or its numeric unique user ID.
	MemberKey string `json:"memberKey"`
}

// RemoveUserFromGroupOutput confirms the account is no longer a direct member.
type RemoveUserFromGroupOutput struct {
	// GroupKey is the group key the Step requested.
	GroupKey string `json:"groupKey"`
	// MemberKey is the member key the Step requested.
	MemberKey string `json:"memberKey"`
	// WasAlreadyRemoved reports that the account was not a direct member when this attempt ran.
	WasAlreadyRemoved bool `json:"wasAlreadyRemoved"`
}

// AddUserToGroupOperation implements the addUserToGroup connector Mutation.
type AddUserToGroupOperation struct{ client *Client }

// RemoveUserFromGroupOperation implements the removeUserFromGroup connector Mutation.
type RemoveUserFromGroupOperation struct{ client *Client }

type directoryMember struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

var addUserToGroupFailureBranches = failureBranches{
	notFound: AddUserToGroupBranchNotFound, conflict: AddUserToGroupBranchProviderRejected, rejected: AddUserToGroupBranchProviderRejected,
	invalidResponse: AddUserToGroupBranchInvalidResponse, defect: AddUserToGroupBranchDefect,
}

var removeUserFromGroupFailureBranches = failureBranches{
	notFound: RemoveUserFromGroupBranchNotFound, conflict: RemoveUserFromGroupBranchProviderRejected, rejected: RemoveUserFromGroupBranchProviderRejected,
	invalidResponse: RemoveUserFromGroupBranchInvalidResponse, defect: RemoveUserFromGroupBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (AddUserToGroupOperation) Definition() sdkgo.MutationDefinition {
	return AddUserToGroupDefinition
}

// IdempotencyKey returns the Call ID; Google allows one direct membership per account and group.
func (AddUserToGroupOperation) IdempotencyKey(callID sdkgo.CallID, _ AddUserToGroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke inserts the membership. When Google reports it already exists, it reads the
// membership back and returns it, so a repeated or concurrent attempt converges on one
// membership. A membership insert that races a just-created account is retried.
func (operation AddUserToGroupOperation) Invoke(call sdkgo.Call, input AddUserToGroupInput) sdkgo.MutationAttempt[AddUserToGroupOutput] {
	const operationID = "addUserToGroup"
	groupKey, memberEmail, role, err := validateAddUserToGroupInput(input)
	if err != nil {
		return sdkgo.NewMutationBranch(AddUserToGroupBranchDefect, AddUserToGroupOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(AddUserToGroupBranchDefect, AddUserToGroupOutput{}, failure, sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, &credential, operationID, directoryRequest{
		method: http.MethodPost, path: groupMembersPath(groupKey), payload: map[string]string{"email": memberEmail, "role": string(role)},
	})
	switch exchange.outcome {
	case exchangeSucceeded:
		membership, err := decodeGroupMembership(exchange.response.body, groupKey)
		if err != nil {
			return sdkgo.NewMutationRetry[AddUserToGroupOutput](directoryFailure(sdkgo.FailureProtocol, operationID, "insert response is unusable; the retry reads the membership back"), 0)
		}
		return sdkgo.NewMutationBranch(AddUserToGroupBranchAdded, AddUserToGroupOutput{Membership: membership}, nil, operation.client.receipt(call, exchange.response, membership.MemberID))
	case exchangeConflict:
		return operation.convergeOnExistingMembership(call, &credential, groupKey, memberEmail)
	case exchangeInvalidResponse:
		return sdkgo.NewMutationRetry[AddUserToGroupOutput](directoryFailure(exchange.failure.Kind, operationID, "insert response is unusable; the retry reads the membership back"), 0)
	default:
		return mutationAttemptFromExchange[AddUserToGroupOutput](operation.client, call, exchange, addUserToGroupFailureBranches)
	}
}

// convergeOnExistingMembership reads the membership behind a 409 and returns it unchanged.
func (operation AddUserToGroupOperation) convergeOnExistingMembership(call sdkgo.Call, credential *Credentials, groupKey string, memberEmail string) sdkgo.MutationAttempt[AddUserToGroupOutput] {
	const operationID = "addUserToGroup"
	exchange := operation.client.exchange(call, credential, operationID, directoryRequest{method: http.MethodGet, path: groupMemberPath(groupKey, memberEmail)})
	switch exchange.outcome {
	case exchangeSucceeded:
		receipt := operation.client.receipt(call, exchange.response, "")
		membership, err := decodeGroupMembership(exchange.response.body, groupKey)
		if err != nil {
			return sdkgo.NewMutationBranch(AddUserToGroupBranchInvalidResponse, AddUserToGroupOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid membership: "+err.Error()), receipt)
		}
		receipt.ProviderObjectID = membership.MemberID
		return sdkgo.NewMutationBranch(AddUserToGroupBranchAdded, AddUserToGroupOutput{Membership: membership, WasAlreadyMember: true}, nil, receipt)
	case exchangeNotFound:
		return sdkgo.NewMutationRetry[AddUserToGroupOutput](directoryFailure(sdkgo.FailureAvailability, operationID, "Google reports the membership as existing but does not return it yet"), 0)
	default:
		return mutationAttemptFromExchange[AddUserToGroupOutput](operation.client, call, exchange, addUserToGroupFailureBranches)
	}
}

// Definition returns the immutable connector operation definition.
func (RemoveUserFromGroupOperation) Definition() sdkgo.MutationDefinition {
	return RemoveUserFromGroupDefinition
}

// IdempotencyKey returns the Call ID; a removed membership stays removed.
func (RemoveUserFromGroupOperation) IdempotencyKey(callID sdkgo.CallID, _ RemoveUserFromGroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the direct membership. Google answers 404 both for a missing group and for an
// account that is not a member, so a 404 is followed by a one-member group read: an existing group
// means the membership is already gone, and a missing group selects notFound. Nested memberships
// through another group are not changed.
func (operation RemoveUserFromGroupOperation) Invoke(call sdkgo.Call, input RemoveUserFromGroupInput) sdkgo.MutationAttempt[RemoveUserFromGroupOutput] {
	const operationID = "removeUserFromGroup"
	groupKey, err := validateGroupKey(input.GroupKey)
	if err != nil {
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchDefect, RemoveUserFromGroupOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	memberKey, err := validateUserKey(input.MemberKey)
	if err != nil {
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchDefect, RemoveUserFromGroupOutput{}, failurePointer(sdkgo.FailureValidation, operationID, "memberKey must be one bare email address or a numeric user ID"), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchDefect, RemoveUserFromGroupOutput{}, failure, sdkgo.Receipt{})
	}
	output := RemoveUserFromGroupOutput{GroupKey: groupKey, MemberKey: memberKey}
	exchange := operation.client.exchange(call, &credential, operationID, directoryRequest{method: http.MethodDelete, path: groupMemberPath(groupKey, memberKey)})
	switch exchange.outcome {
	case exchangeSucceeded:
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchRemoved, output, nil, operation.client.receipt(call, exchange.response, ""))
	case exchangeInvalidResponse:
		return sdkgo.NewMutationRetry[RemoveUserFromGroupOutput](directoryFailure(exchange.failure.Kind, operationID, "delete response is unusable; the retry confirms the removal"), 0)
	case exchangeNotFound:
		groupCheck := operation.client.exchange(call, &credential, operationID, directoryRequest{
			method: http.MethodGet, path: groupMembersPath(groupKey), query: url.Values{"maxResults": {"1"}},
		})
		if groupCheck.outcome != exchangeSucceeded {
			return mutationAttemptFromExchange[RemoveUserFromGroupOutput](operation.client, call, groupCheck, removeUserFromGroupFailureBranches)
		}
		output.WasAlreadyRemoved = true
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchRemoved, output, nil, operation.client.receipt(call, groupCheck.response, ""))
	default:
		return mutationAttemptFromExchange[RemoveUserFromGroupOutput](operation.client, call, exchange, removeUserFromGroupFailureBranches)
	}
}

func validateAddUserToGroupInput(input AddUserToGroupInput) (string, string, GroupRole, error) {
	groupKey, err := validateGroupKey(input.GroupKey)
	if err != nil {
		return "", "", "", err
	}
	memberEmail := strings.TrimSpace(input.MemberEmail)
	if !isBareEmailAddress(memberEmail) {
		return "", "", "", errors.New("memberEmail must be one bare email address")
	}
	role := input.Role
	if role == "" {
		role = GroupRoleMember
	}
	if !isGroupRole(role) {
		return "", "", "", errors.New("role must be MEMBER, MANAGER, or OWNER")
	}
	return groupKey, memberEmail, role, nil
}

// decodeGroupMembership validates one untrusted Directory API member resource.
func decodeGroupMembership(body []byte, groupKey string) (GroupMembership, error) {
	var resource directoryMember
	if err := json.Unmarshal(body, &resource); err != nil {
		return GroupMembership{}, errors.New("membership is not valid JSON")
	}
	role := GroupRole(resource.Role)
	if resource.ID == "" || len(resource.ID) > 64 || !isGroupRole(role) {
		return GroupMembership{}, errors.New("membership lacks a valid member ID or role")
	}
	return GroupMembership{
		GroupKey: groupKey, MemberID: resource.ID, Email: resource.Email, Role: role, Type: resource.Type, Status: resource.Status,
	}, nil
}

func isGroupRole(role GroupRole) bool {
	return role == GroupRoleMember || role == GroupRoleManager || role == GroupRoleOwner
}
