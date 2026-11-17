// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SubscribeContactToListOperation activates one contact's list membership.
type SubscribeContactToListOperation struct{ client *Client }

// UnsubscribeContactFromListOperation deactivates one contact's list membership.
type UnsubscribeContactFromListOperation struct{ client *Client }

type contactListChangeBranches struct{ success, rejected, invalid, defect sdkgo.BranchID }

type contactListDispatch struct {
	client       *Client
	call         sdkgo.Call
	body         []byte
	responseBody []byte
	status       int
	retryAfter   time.Duration
	err          error
}

// SubscribeContactToList binds the subscribe Mutation to this client.
func (client *Client) SubscribeContactToList() SubscribeContactToListOperation {
	return SubscribeContactToListOperation{client}
}

// UnsubscribeContactFromList binds the unsubscribe Mutation to this client.
func (client *Client) UnsubscribeContactFromList() UnsubscribeContactFromListOperation {
	return UnsubscribeContactFromListOperation{client}
}

// Definition returns the manifest-generated subscribe contract.
func (SubscribeContactToListOperation) Definition() sdkgo.MutationDefinition {
	return SubscribeContactToListDefinition
}

// Definition returns the manifest-generated unsubscribe contract.
func (UnsubscribeContactFromListOperation) Definition() sdkgo.MutationDefinition {
	return UnsubscribeContactFromListDefinition
}

// IdempotencyKey correlates bounded sends locally; ActiveCampaign does not receive this key.
func (SubscribeContactToListOperation) IdempotencyKey(callID sdkgo.CallID, _ SetContactListMembershipInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates bounded sends locally; ActiveCampaign does not receive this key.
func (UnsubscribeContactFromListOperation) IdempotencyKey(callID sdkgo.CallID, _ SetContactListMembershipInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke activates a membership, reading first to avoid repeat subscription side effects.
func (operation SubscribeContactToListOperation) Invoke(call sdkgo.Call, input SetContactListMembershipInput) sdkgo.MutationAttempt[ContactListMembership] {
	branches := contactListChangeBranches{success: SubscribeContactToListBranchSubscribed, rejected: SubscribeContactToListBranchProviderRejected, invalid: SubscribeContactToListBranchInvalidResponse, defect: SubscribeContactToListBranchDefect}
	return changeContactListStatus(call, operation.client, "subscribeContactToList", input, 1, SubscribeContactToListDefinition, branches)
}

// Invoke deactivates a membership, reading first to avoid repeat unsubscribe side effects.
func (operation UnsubscribeContactFromListOperation) Invoke(call sdkgo.Call, input SetContactListMembershipInput) sdkgo.MutationAttempt[ContactListMembership] {
	branches := contactListChangeBranches{success: UnsubscribeContactFromListBranchUnsubscribed, rejected: UnsubscribeContactFromListBranchProviderRejected, invalid: UnsubscribeContactFromListBranchInvalidResponse, defect: UnsubscribeContactFromListBranchDefect}
	return changeContactListStatus(call, operation.client, "unsubscribeContactFromList", input, 2, UnsubscribeContactFromListDefinition, branches)
}

func changeContactListStatus(call sdkgo.Call, client *Client, operation string, input SetContactListMembershipInput, targetStatus int, definition sdkgo.MutationDefinition, branches contactListChangeBranches) sdkgo.MutationAttempt[ContactListMembership] {
	if !numericID.MatchString(input.ContactID) || !numericID.MatchString(input.ListID) {
		return contactListChangeBranch(operation, branches.defect, sdkgo.FailureValidation, "contact or list ID must be numeric")
	}
	membership, state, kind, retryAfter := readCurrentContactListMembership(call, client, input)
	switch state {
	case "retry":
		return sdkgo.NewMutationRetry[ContactListMembership](queryFailure(operation, kind, "membership read-back is temporarily unavailable"), retryAfter)
	case "defect":
		return contactListChangeBranch(operation, branches.defect, kind, "connection credentials are unavailable or invalid")
	case "rejected":
		return contactListChangeBranch(operation, branches.rejected, kind, "ActiveCampaign rejected the membership read")
	case "invalid":
		return contactListChangeBranch(operation, branches.invalid, kind, "ActiveCampaign returned invalid membership data")
	}
	if membership.Status == targetStatus && state == "found" {
		return sdkgo.NewMutationBranch(branches.success, membership, nil, dealReceipt(call, membership.ID))
	}
	if state == "absent" && targetStatus == 2 {
		value := ContactListMembership{ContactID: input.ContactID, ListID: input.ListID, Status: 2}
		return sdkgo.NewMutationBranch(branches.success, value, nil, dealReceipt(call, ""))
	}
	encoded, err := json.Marshal(map[string]any{"contactList": map[string]any{"contact": input.ContactID, "list": input.ListID, "status": targetStatus}})
	if err != nil {
		return contactListChangeBranch(operation, branches.defect, sdkgo.FailureLocalDefect, "membership change could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return contactListChangeBranch(operation, branches.defect, sdkgo.FailureLocalDefect, "membership checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return contactListChangeBranch(operation, branches.defect, sdkgo.FailureLocalDefect, "membership checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return contactListChangeUncertain(call, operation, "a prior membership change may have succeeded and reached the send cap")
	}
	dispatch := &contactListDispatch{client: client, call: call, body: encoded}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[ContactListMembership](queryFailure(operation, sdkgo.FailureAvailability, "membership checkpoint is unavailable"), 0)
	}
	if errors.Is(dispatch.err, errCredential) {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[ContactListMembership](queryFailure(operation, sdkgo.FailureAvailability, "membership retry checkpoint is unavailable"), 0)
		}
		return contactListChangeBranch(operation, branches.defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(dispatch.err, errRequest) {
		return sdkgo.NewMutationRetry[ContactListMembership](queryFailure(operation, sdkgo.FailureTransport, "membership change outcome is unknown"), 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[ContactListMembership](queryFailure(operation, sdkgo.FailureAvailability, "membership retry checkpoint is unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if dispatch.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[ContactListMembership](queryFailure(operation, kind, "ActiveCampaign temporarily rejected the membership change"), dispatch.retryAfter)
	}
	if dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		return sdkgo.NewMutationRetry[ContactListMembership](queryFailure(operation, sdkgo.FailureAvailability, "membership change outcome is unknown"), dispatch.retryAfter)
	}
	if dispatch.status != http.StatusOK && dispatch.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return contactListChangeUncertain(call, operation, "a prior membership change may have succeeded despite this rejection")
		}
		return contactListChangeBranch(operation, branches.rejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(dispatch.status))
	}
	if dispatch.err != nil {
		return contactListChangeUncertain(call, operation, "ActiveCampaign accepted the membership change but response was incomplete")
	}
	value, err := decodeContactListMembership(dispatch.responseBody)
	if err != nil || value.ContactID != input.ContactID || value.ListID != input.ListID || value.Status != targetStatus {
		return contactListChangeUncertain(call, operation, "ActiveCampaign accepted the membership change but returned invalid JSON")
	}
	_ = fence.RecordProviderID(value.ID)
	return sdkgo.NewMutationBranch(branches.success, value, nil, dealReceipt(call, value.ID))
}

func readCurrentContactListMembership(call sdkgo.Call, client *Client, input SetContactListMembershipInput) (ContactListMembership, string, sdkgo.FailureKind, time.Duration) {
	query := url.Values{"contact": {input.ContactID}, "limit": {"100"}, "offset": {"0"}}
	body, status, retryAfter, err := client.get(call, "/contactLists?"+query.Encode())
	if errors.Is(err, errCredential) {
		return ContactListMembership{}, "defect", sdkgo.FailureAuthentication, 0
	}
	if errors.Is(err, errRequest) {
		return ContactListMembership{}, "retry", sdkgo.FailureTransport, 0
	}
	if status == http.StatusTooManyRequests {
		return ContactListMembership{}, "retry", sdkgo.FailureRateLimit, retryAfter
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return ContactListMembership{}, "retry", sdkgo.FailureAvailability, retryAfter
	}
	if status != http.StatusOK {
		return ContactListMembership{}, "rejected", sdkgo.FailureProviderRejection, 0
	}
	if err != nil {
		return ContactListMembership{}, "invalid", sdkgo.FailureProtocol, 0
	}
	page, err := decodeContactListMemberships(body, ListContactListMembershipsInput{ContactID: input.ContactID, Limit: 100})
	if err != nil || page.Total > len(page.Memberships) {
		return ContactListMembership{}, "invalid", sdkgo.FailureProtocol, 0
	}
	var selected ContactListMembership
	for _, membership := range page.Memberships {
		if membership.ListID == input.ListID {
			if selected.ID != "" {
				return ContactListMembership{}, "invalid", sdkgo.FailureProtocol, 0
			}
			selected = membership
		}
	}
	if selected.ID != "" {
		return selected, "found", "", 0
	}
	return ContactListMembership{}, "absent", "", 0
}

func (dispatch *contactListDispatch) execute(ctx context.Context) error {
	dispatch.responseBody, dispatch.status, dispatch.retryAfter, dispatch.err = dispatch.client.requestWithContext(ctx, dispatch.call, http.MethodPost, "/contactLists", dispatch.body)
	return nil
}

func contactListChangeUncertain(call sdkgo.Call, operation, message string) sdkgo.MutationAttempt[ContactListMembership] {
	return sdkgo.NewMutationUncertain(ContactListMembership{}, queryFailure(operation, sdkgo.FailureTransport, message), dealReceipt(call, ""))
}

func contactListChangeBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContactListMembership] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ContactListMembership{}, &failure, sdkgo.Receipt{})
}
