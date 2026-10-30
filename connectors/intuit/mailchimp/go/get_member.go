// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getMemberOperation = "getMember"

// GetMemberInput identifies one audience contact.
type GetMemberInput struct {
	// ListID is the audience ID, Mailchimp's list_id, such as 57afe96172.
	ListID string `json:"listId"`
	// EmailAddress is the contact's bare email address. The connector requests the MD5 hash of its
	// lowercased form, so letter case does not matter.
	EmailAddress string `json:"emailAddress"`
}

// GetMemberOperation is the getMember Query.
type GetMemberOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetMemberOperation) Definition() sdkgo.QueryDefinition { return GetMemberDefinition }

// Invoke reads GET /lists/{list_id}/members/{subscriber_hash}. A contact in any status, including
// unsubscribed, cleaned, or archived, selects found; Mailchimp's 404 selects notFound.
func (operation GetMemberOperation) Invoke(call sdkgo.Call, input GetMemberInput) sdkgo.QueryAttempt[Member] {
	if err := validateMemberAddress(input.ListID, input.EmailAddress); err != nil {
		return sdkgo.NewQueryBranch(GetMemberBranchDefect, Member{}, mailchimpFailurePointer(sdkgo.FailureValidation, getMemberOperation, err.Error()), sdkgo.Receipt{})
	}
	connection, failure := operation.client.resolveConnection(call, getMemberOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetMemberBranchDefect, Member{}, failure, sdkgo.Receipt{})
	}
	subscriberHash := SubscriberHash(input.EmailAddress)
	result := operation.client.exchange(call, connection, getMemberOperation, mailchimpRequest{
		method: http.MethodGet, path: memberPath(input.ListID, subscriberHash),
	})
	receipt := operation.client.receipt(call, result.response, subscriberHash)
	if attempt, isTerminal := queryAttemptForExchange[Member](result, receipt, repeatableBranches{
		notFound: GetMemberBranchNotFound, providerRejected: GetMemberBranchProviderRejected,
		invalidResponse: GetMemberBranchInvalidResponse, defect: GetMemberBranchDefect,
	}); isTerminal {
		return attempt
	}
	member, err := decodeMemberBody(result.response.body, subscriberHash)
	if err != nil {
		return sdkgo.NewQueryBranch(GetMemberBranchInvalidResponse, Member{}, mailchimpFailurePointer(sdkgo.FailureProtocol, getMemberOperation,
			"Mailchimp returned an invalid contact: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetMemberBranchFound, member, nil, receipt)
}

func validateMemberAddress(listID string, emailAddress string) error {
	if err := validateResourceID("listId", listID); err != nil {
		return err
	}
	return validateEmailAddress("emailAddress", emailAddress)
}

func memberPath(listID string, subscriberHash string) string {
	return "/lists/" + listID + "/members/" + subscriberHash
}
