// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	upsertMemberOperation = "upsertMember"

	// MaxMarketingPermissions bounds the marketing permissions one upsertMember sets.
	MaxMarketingPermissions = 20
)

// UpsertMemberInput adds a contact to an audience or updates the existing contact with the same
// address. Mailchimp applies StatusIfNew only when it creates the contact, and Status to an existing
// contact too, so an unsubscribed contact keeps its status unless Status says otherwise.
type UpsertMemberInput struct {
	// ListID is the audience ID, Mailchimp's list_id.
	ListID string `json:"listId"`
	// EmailAddress is the contact's bare email address; letter case does not change which contact is written.
	EmailAddress string `json:"emailAddress"`
	// StatusIfNew is the status of a contact Mailchimp creates: subscribed for a contact who gave
	// permission, pending to send Mailchimp's confirmation email first, unsubscribed, or
	// transactional. It is required and ignored for an existing contact.
	StatusIfNew MemberStatus `json:"statusIfNew"`
	// Status, when set, also changes an existing contact's status: subscribed, pending, unsubscribed,
	// or transactional. Empty keeps the existing contact's status. Unsubscribed records an opt-out.
	Status MemberStatus `json:"status,omitempty"`
	// IsResubscribeAllowed must be true when Status is subscribed or pending, because Mailchimp then
	// resubscribes, or emails a confirmation to, a contact who unsubscribed or was cleaned. Set it
	// only when the contact asked to subscribe again.
	IsResubscribeAllowed bool `json:"isResubscribeAllowed,omitempty"`
	// MergeFields maps merge tags such as FNAME to values: a string, a number, or an address object
	// with addr1, city, state, and zip. Tags that are omitted keep their stored values.
	MergeFields map[string]any `json:"mergeFields,omitempty"`
	// MarketingPermissions records the contact's consent to the audience's GDPR marketing permissions.
	MarketingPermissions []MarketingPermissionInput `json:"marketingPermissions,omitempty"`
	// ShouldSkipMergeValidation sends skip_merge_validation=true, so Mailchimp accepts the contact
	// without the audience's required merge fields.
	ShouldSkipMergeValidation bool `json:"shouldSkipMergeValidation,omitempty"`
}

// MarketingPermissionInput is one GDPR marketing permission the contact opted in to or out of.
type MarketingPermissionInput struct {
	// MarketingPermissionID is the audience's marketing_permission_id, listed on a contact read with
	// marketing permissions enabled.
	MarketingPermissionID string `json:"marketingPermissionId"`
	// IsEnabled reports that the contact opted in.
	IsEnabled bool `json:"isEnabled"`
}

// UpsertMemberOutput is the contact after the write.
type UpsertMemberOutput struct {
	// Member is the contact Mailchimp returned. Its Status shows whether an existing contact kept
	// its status, such as unsubscribed.
	Member Member `json:"member"`
}

// UpsertMemberOperation is the upsertMember Mutation.
type UpsertMemberOperation struct {
	client *Client
}

type upsertMemberRequestWire struct {
	EmailAddress         string                         `json:"email_address"`
	StatusIfNew          MemberStatus                   `json:"status_if_new"`
	Status               MemberStatus                   `json:"status,omitempty"`
	MergeFields          map[string]any                 `json:"merge_fields,omitempty"`
	MarketingPermissions []marketingPermissionInputWire `json:"marketing_permissions,omitempty"`
}

type marketingPermissionInputWire struct {
	MarketingPermissionID string `json:"marketing_permission_id"`
	Enabled               bool   `json:"enabled"`
}

// Definition returns the immutable connector operation definition.
func (UpsertMemberOperation) Definition() sdkgo.MutationDefinition { return UpsertMemberDefinition }

// IdempotencyKey uses the stable connector Call ID. Mailchimp documents no idempotency key; the PUT is
// keyed by the contact's address and sets absolute values, so the key only correlates the Receipt.
func (UpsertMemberOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertMemberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends PUT /lists/{list_id}/members/{subscriber_hash}. A retried or concurrently dispatched
// attempt finds the contact it created and writes the same values, so every unconfirmed outcome,
// including a 5xx or a lost response, is retried. Because status_if_new applies only to a contact
// Mailchimp creates, a repeated PUT without Status leaves the contact's status as the first one set it.
func (operation UpsertMemberOperation) Invoke(call sdkgo.Call, input UpsertMemberInput) sdkgo.MutationAttempt[UpsertMemberOutput] {
	if err := validateUpsertMemberInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpsertMemberBranchDefect, UpsertMemberOutput{}, mailchimpFailurePointer(sdkgo.FailureValidation, upsertMemberOperation, err.Error()), sdkgo.Receipt{})
	}
	connection, failure := operation.client.resolveConnection(call, upsertMemberOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpsertMemberBranchDefect, UpsertMemberOutput{}, failure, sdkgo.Receipt{})
	}
	subscriberHash := SubscriberHash(input.EmailAddress)
	request := mailchimpRequest{method: http.MethodPut, path: memberPath(input.ListID, subscriberHash), payload: buildUpsertMemberRequest(input)}
	if input.ShouldSkipMergeValidation {
		request.query = url.Values{"skip_merge_validation": {"true"}}
	}
	result := operation.client.exchange(call, connection, upsertMemberOperation, request)
	receipt := operation.client.receipt(call, result.response, subscriberHash)
	if attempt, isTerminal := repeatableMutationAttemptForExchange[UpsertMemberOutput](result, receipt, repeatableBranches{
		providerRejected: UpsertMemberBranchProviderRejected, invalidResponse: UpsertMemberBranchInvalidResponse, defect: UpsertMemberBranchDefect,
	}); isTerminal {
		return attempt
	}
	member, err := decodeMemberBody(result.response.body, subscriberHash)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertMemberBranchInvalidResponse, UpsertMemberOutput{}, mailchimpFailurePointer(sdkgo.FailureProtocol, upsertMemberOperation,
			"Mailchimp accepted the contact but returned an invalid contact: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpsertMemberBranchUpserted, UpsertMemberOutput{Member: member}, nil, receipt)
}

func validateUpsertMemberInput(input UpsertMemberInput) error {
	if err := validateMemberAddress(input.ListID, input.EmailAddress); err != nil {
		return err
	}
	if !IsWritableMemberStatus(input.StatusIfNew) {
		return errors.New("statusIfNew is required: subscribed, pending, unsubscribed, or transactional")
	}
	if input.Status != "" && !IsWritableMemberStatus(input.Status) {
		return errors.New("status must be subscribed, pending, unsubscribed, or transactional, or empty to keep an existing contact's status")
	}
	isResubscribing := input.Status == MemberStatusSubscribed || input.Status == MemberStatusPending
	if isResubscribing && !input.IsResubscribeAllowed {
		return fmt.Errorf("status %s resubscribes a contact who unsubscribed, so it requires isResubscribeAllowed; use statusIfNew to subscribe only new contacts", input.Status)
	}
	if err := validateMergeFields(input.MergeFields); err != nil {
		return err
	}
	if len(input.MarketingPermissions) > MaxMarketingPermissions {
		return fmt.Errorf("marketingPermissions holds at most %d permissions", MaxMarketingPermissions)
	}
	seen := map[string]bool{}
	for _, permission := range input.MarketingPermissions {
		if err := validateResourceID("marketingPermissionId", permission.MarketingPermissionID); err != nil {
			return err
		}
		if seen[permission.MarketingPermissionID] {
			return fmt.Errorf("marketing permission %s repeats", permission.MarketingPermissionID)
		}
		seen[permission.MarketingPermissionID] = true
	}
	return nil
}

func buildUpsertMemberRequest(input UpsertMemberInput) upsertMemberRequestWire {
	request := upsertMemberRequestWire{
		EmailAddress: input.EmailAddress, StatusIfNew: input.StatusIfNew, Status: input.Status, MergeFields: input.MergeFields,
	}
	for _, permission := range input.MarketingPermissions {
		request.MarketingPermissions = append(request.MarketingPermissions, marketingPermissionInputWire{
			MarketingPermissionID: permission.MarketingPermissionID, Enabled: permission.IsEnabled,
		})
	}
	return request
}
