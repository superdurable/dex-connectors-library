// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// graphDirectoryObjectsURL is the reference base Microsoft requires in @odata.id; it never follows a local provider URL.
const graphDirectoryObjectsURL = graphBaseURL + "/directoryObjects/"

// AddUserToGroupInput adds one account to one group.
type AddUserToGroupInput struct {
	// GroupID is the group's object ID, a GUID shown at https://entra.microsoft.com > Entra ID > Groups > the group > Overview.
	GroupID string `json:"groupId"`
	// UserID is the account's object ID, such as the ID createUser or getUser returned.
	UserID string `json:"userId"`
}

// AddUserToGroupOutput confirms that the account is a direct member of the group.
type AddUserToGroupOutput struct {
	// GroupID is the group's object ID in lowercase.
	GroupID string `json:"groupId"`
	// UserID is the account's object ID in lowercase.
	UserID string `json:"userId"`
	// WasAlreadyMember reports that the account was already a direct member when this attempt ran.
	WasAlreadyMember bool `json:"wasAlreadyMember"`
}

// RemoveUserFromGroupInput removes one account's direct membership from one group.
type RemoveUserFromGroupInput struct {
	// GroupID is the group's object ID, a GUID.
	GroupID string `json:"groupId"`
	// UserID is the account's object ID.
	UserID string `json:"userId"`
}

// RemoveUserFromGroupOutput confirms that the account is no longer a direct member of the group.
type RemoveUserFromGroupOutput struct {
	// GroupID is the group's object ID in lowercase.
	GroupID string `json:"groupId"`
	// UserID is the account's object ID in lowercase.
	UserID string `json:"userId"`
	// WasAlreadyRemoved reports that the account was not a direct member when this attempt ran.
	WasAlreadyRemoved bool `json:"wasAlreadyRemoved"`
}

// AddUserToGroupOperation implements the addUserToGroup connector Mutation.
type AddUserToGroupOperation struct{ client *Client }

// RemoveUserFromGroupOperation implements the removeUserFromGroup connector Mutation.
type RemoveUserFromGroupOperation struct{ client *Client }

type graphDirectoryObjectPage struct {
	Value []struct {
		ID string `json:"id"`
	} `json:"value"`
}

var addUserToGroupFailureBranches = failureBranches{
	notFound: AddUserToGroupBranchNotFound, rejected: AddUserToGroupBranchProviderRejected,
	invalidResponse: AddUserToGroupBranchInvalidResponse, defect: AddUserToGroupBranchDefect,
}

var removeUserFromGroupFailureBranches = failureBranches{
	notFound: RemoveUserFromGroupBranchNotFound, rejected: RemoveUserFromGroupBranchProviderRejected,
	invalidResponse: RemoveUserFromGroupBranchInvalidResponse, defect: RemoveUserFromGroupBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (AddUserToGroupOperation) Definition() sdkgo.MutationDefinition {
	return AddUserToGroupDefinition
}

// IdempotencyKey returns the Call ID; a group holds at most one direct membership per account.
func (AddUserToGroupOperation) IdempotencyKey(callID sdkgo.CallID, _ AddUserToGroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds the membership. Microsoft answers 400 for an existing member, an unsupported member,
// and a group that has not replicated yet, and asks callers not to read error messages, so every 400
// checks the group's direct members: an existing membership is reported as added, so a repeated or
// concurrent attempt converges on one membership. A 404 checks whether the group exists, because
// Microsoft also answers 404 for an account that has not replicated yet; both lags are retried
// within the propagation window.
func (operation AddUserToGroupOperation) Invoke(call sdkgo.Call, input AddUserToGroupInput) sdkgo.MutationAttempt[AddUserToGroupOutput] {
	const operationID = "addUserToGroup"
	groupID, userID, err := validateMembershipInput(input.GroupID, input.UserID)
	if err != nil {
		return sdkgo.NewMutationBranch(AddUserToGroupBranchDefect, AddUserToGroupOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(AddUserToGroupBranchDefect, AddUserToGroupOutput{}, failure, sdkgo.Receipt{})
	}
	output := AddUserToGroupOutput{GroupID: groupID, UserID: userID}
	exchange := operation.client.exchange(call, &credential, operationID, graphRequest{
		method: http.MethodPost, path: groupMembersPath(groupID) + "/$ref", payload: map[string]string{"@odata.id": graphDirectoryObjectsURL + userID},
	})
	switch exchange.outcome {
	case exchangeSucceeded:
		return sdkgo.NewMutationBranch(AddUserToGroupBranchAdded, output, nil, operation.client.receipt(call, exchange.response, userID))
	case exchangeBadRequest:
		return operation.confirmExistingMembership(call, &credential, output, exchange)
	case exchangeNotFound:
		return operation.confirmMissingMember(call, &credential, output)
	default:
		return mutationAttemptFromExchange[AddUserToGroupOutput](operation.client, call, exchange, addUserToGroupFailureBranches)
	}
}

// confirmExistingMembership checks direct members behind a 400 through Microsoft's eventually consistent index.
func (operation AddUserToGroupOperation) confirmExistingMembership(
	call sdkgo.Call, credential *Credentials, output AddUserToGroupOutput, addExchange graphExchange,
) sdkgo.MutationAttempt[AddUserToGroupOutput] {
	const operationID = "addUserToGroup"
	check := operation.client.exchange(call, credential, operationID, graphRequest{
		method: http.MethodGet, path: groupMembersPath(output.GroupID), usesEventualConsistency: true,
		query: url.Values{"$count": {"true"}, "$filter": {"id eq '" + output.UserID + "'"}, "$select": {"id"}},
	})
	if check.outcome != exchangeSucceeded {
		return mutationAttemptFromExchange[AddUserToGroupOutput](operation.client, call, check, addUserToGroupFailureBranches)
	}
	receipt := operation.client.receipt(call, check.response, output.UserID)
	isMember, err := pageContainsObject(check.response.body, output.UserID)
	if err != nil {
		return sdkgo.NewMutationBranch(AddUserToGroupBranchInvalidResponse, AddUserToGroupOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid membership check"), receipt)
	}
	if isMember {
		output.WasAlreadyMember = true
		return sdkgo.NewMutationBranch(AddUserToGroupBranchAdded, output, nil, receipt)
	}
	if operation.client.isWithinPropagationWindow(call) {
		return sdkgo.NewMutationRetry[AddUserToGroupOutput](graphFailure(sdkgo.FailureAvailability, operationID, "Microsoft Graph rejected the membership but does not list it yet"), 0)
	}
	failure := addExchange.failure
	return sdkgo.NewMutationBranch(AddUserToGroupBranchProviderRejected, AddUserToGroupOutput{}, &failure, operation.client.receipt(call, addExchange.response, ""))
}

// confirmMissingMember distinguishes a missing group from an account that has not replicated yet.
func (operation AddUserToGroupOperation) confirmMissingMember(call sdkgo.Call, credential *Credentials, output AddUserToGroupOutput) sdkgo.MutationAttempt[AddUserToGroupOutput] {
	const operationID = "addUserToGroup"
	groupCheck := operation.client.readGroup(call, credential, operationID, output.GroupID)
	if groupCheck.outcome != exchangeSucceeded {
		return mutationAttemptFromExchange[AddUserToGroupOutput](operation.client, call, groupCheck, addUserToGroupFailureBranches)
	}
	if operation.client.isWithinPropagationWindow(call) {
		return sdkgo.NewMutationRetry[AddUserToGroupOutput](graphFailure(sdkgo.FailureAvailability, operationID, "Microsoft Graph does not return the account yet"), 0)
	}
	return sdkgo.NewMutationBranch(AddUserToGroupBranchNotFound, AddUserToGroupOutput{},
		failurePointer(sdkgo.FailureNotFound, operationID, "account was not found"), operation.client.receipt(call, groupCheck.response, ""))
}

// Definition returns the immutable connector operation definition.
func (RemoveUserFromGroupOperation) Definition() sdkgo.MutationDefinition {
	return RemoveUserFromGroupDefinition
}

// IdempotencyKey returns the Call ID; a removed membership stays removed.
func (RemoveUserFromGroupOperation) IdempotencyKey(callID sdkgo.CallID, _ RemoveUserFromGroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the membership reference, never the account: the path always ends in /$ref. A 404
// is followed by a group read: an existing group means the membership is already gone, and a missing
// group selects notFound. Membership through a nested group is not changed.
func (operation RemoveUserFromGroupOperation) Invoke(call sdkgo.Call, input RemoveUserFromGroupInput) sdkgo.MutationAttempt[RemoveUserFromGroupOutput] {
	const operationID = "removeUserFromGroup"
	groupID, userID, err := validateMembershipInput(input.GroupID, input.UserID)
	if err != nil {
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchDefect, RemoveUserFromGroupOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchDefect, RemoveUserFromGroupOutput{}, failure, sdkgo.Receipt{})
	}
	output := RemoveUserFromGroupOutput{GroupID: groupID, UserID: userID}
	exchange := operation.client.exchange(call, &credential, operationID, graphRequest{method: http.MethodDelete, path: groupMemberReferencePath(groupID, userID)})
	switch exchange.outcome {
	case exchangeSucceeded:
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchRemoved, output, nil, operation.client.receipt(call, exchange.response, userID))
	case exchangeNotFound:
		groupCheck := operation.client.readGroup(call, &credential, operationID, groupID)
		if groupCheck.outcome != exchangeSucceeded {
			return mutationAttemptFromExchange[RemoveUserFromGroupOutput](operation.client, call, groupCheck, removeUserFromGroupFailureBranches)
		}
		output.WasAlreadyRemoved = true
		return sdkgo.NewMutationBranch(RemoveUserFromGroupBranchRemoved, output, nil, operation.client.receipt(call, groupCheck.response, userID))
	default:
		return mutationAttemptFromExchange[RemoveUserFromGroupOutput](operation.client, call, exchange, removeUserFromGroupFailureBranches)
	}
}

func validateMembershipInput(rawGroupID string, rawUserID string) (string, string, error) {
	groupID, err := validateObjectID("groupId", rawGroupID)
	if err != nil {
		return "", "", err
	}
	userID, err := validateObjectID("userId", rawUserID)
	if err != nil {
		return "", "", err
	}
	return groupID, userID, nil
}

// pageContainsObject reports whether a directory object page lists objectID.
func pageContainsObject(body []byte, objectID string) (bool, error) {
	var page graphDirectoryObjectPage
	if err := json.Unmarshal(body, &page); err != nil {
		return false, err
	}
	for _, object := range page.Value {
		if strings.EqualFold(object.ID, objectID) {
			return true, nil
		}
	}
	return false, nil
}
