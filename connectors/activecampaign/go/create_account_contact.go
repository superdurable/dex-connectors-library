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
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateAccountContactInput links one contact to one CRM account.
type CreateAccountContactInput struct {
	// AccountID is the numeric target account ID.
	AccountID string `json:"accountId"`
	// ContactID is the numeric contact ID; ActiveCampaign allows one account per contact.
	ContactID string `json:"contactId"`
	// JobTitle is the contact's optional role at that account.
	JobTitle string `json:"jobTitle,omitempty"`
}

// CreateAccountContactOperation creates a link with bounded sends.
type CreateAccountContactOperation struct{ client *Client }

type accountContactCreateDispatch struct {
	client     *Client
	call       sdkgo.Call
	body       []byte
	response   []byte
	status     int
	retryAfter time.Duration
	err        error
}

// CreateAccountContact binds the create Mutation.
func (client *Client) CreateAccountContact() CreateAccountContactOperation {
	return CreateAccountContactOperation{client}
}

// Definition returns the manifest-generated create contract.
func (CreateAccountContactOperation) Definition() sdkgo.MutationDefinition {
	return CreateAccountContactDefinition
}

// IdempotencyKey correlates bounded sends locally; the provider receives no key.
func (CreateAccountContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAccountContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func (operation CreateAccountContactOperation) Invoke(call sdkgo.Call, input CreateAccountContactInput) sdkgo.MutationAttempt[AccountContactAssociation] {
	if !numericID.MatchString(input.AccountID) || !numericID.MatchString(input.ContactID) || len(input.JobTitle) > 250 || strings.ContainsAny(input.JobTitle, "\r\n") {
		return accountContactMutationBranch("createAccountContact", CreateAccountContactBranchDefect, sdkgo.FailureValidation, "account ID, contact ID, or job title is invalid")
	}
	encoded, err := json.Marshal(map[string]any{"accountContact": map[string]string{"account": input.AccountID, "contact": input.ContactID, "jobTitle": input.JobTitle}})
	if err != nil {
		return accountContactMutationBranch("createAccountContact", CreateAccountContactBranchDefect, sdkgo.FailureLocalDefect, "account request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateAccountContactDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return accountContactMutationBranch("createAccountContact", CreateAccountContactBranchDefect, sdkgo.FailureLocalDefect, "account-contact checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return accountContactMutationBranch("createAccountContact", CreateAccountContactBranchDefect, sdkgo.FailureLocalDefect, "account-contact checkpoint is invalid")
	}
	if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
		found, state := operation.readBack(call, input, decision.Marker.ProviderID)
		switch state {
		case "found":
			return sdkgo.NewMutationBranch(CreateAccountContactBranchCreated, found, nil, dealReceipt(call, found.ID))
		case "retry":
			return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("createAccountContact", sdkgo.FailureAvailability, "account-contact read-back unavailable"), 0)
		case "uncertain":
			return accountContactCreateUncertain(call, "account-contact read-back cannot uniquely confirm the earlier create")
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return accountContactCreateUncertain(call, "an earlier account-contact link create may have reached the send cap")
	}
	dispatch := &accountContactCreateDispatch{client: operation.client, call: call, body: encoded}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("createAccountContact", sdkgo.FailureAvailability, "account-contact checkpoint unavailable"), 0)
	}
	if errors.Is(dispatch.err, errCredential) {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("createAccountContact", sdkgo.FailureAvailability, "account-contact checkpoint unavailable"), 0)
		}
		return accountContactMutationBranch("createAccountContact", CreateAccountContactBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(dispatch.err, errRequest) {
		return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("createAccountContact", sdkgo.FailureTransport, "account-contact link create outcome is unknown"), 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("createAccountContact", sdkgo.FailureAvailability, "account-contact checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if dispatch.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("createAccountContact", kind, "ActiveCampaign temporarily rejected link creation"), dispatch.retryAfter)
	}
	if dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("createAccountContact", sdkgo.FailureAvailability, "account-contact link create outcome is unknown"), dispatch.retryAfter)
	}
	if dispatch.status != http.StatusCreated && dispatch.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return accountContactCreateUncertain(call, "earlier account-contact link create may have succeeded")
		}
		return accountContactMutationBranch("createAccountContact", CreateAccountContactBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(dispatch.status))
	}
	if dispatch.err != nil {
		return accountContactCreateUncertain(call, "account-contact link create response was interrupted")
	}
	accountContact, err := decodeAccountContact(dispatch.response)
	if err != nil {
		return accountContactCreateUncertain(call, "account-contact link create response was invalid")
	}
	if !matchesAccountContact(accountContact, input) {
		return accountContactCreateUncertain(call, "account-contact link create response does not match the request")
	}
	_ = fence.RecordProviderID(accountContact.ID)
	return sdkgo.NewMutationBranch(CreateAccountContactBranchCreated, accountContact, nil, dealReceipt(call, accountContact.ID))
}

func (operation CreateAccountContactOperation) readBack(call sdkgo.Call, input CreateAccountContactInput, providerID string) (AccountContactAssociation, string) {
	path := "/accountContacts/" + providerID
	if providerID == "" {
		path = "/accountContacts?" + url.Values{"filters[contact]": {input.ContactID}, "limit": {"100"}}.Encode()
	}
	body, status, _, err := operation.client.get(call, path)
	if err != nil || status == http.StatusTooManyRequests || status >= 500 {
		return AccountContactAssociation{}, "retry"
	}
	if status == http.StatusNotFound {
		return AccountContactAssociation{}, "absent"
	}
	if status != http.StatusOK {
		return AccountContactAssociation{}, "uncertain"
	}
	if providerID != "" {
		accountContact, err := decodeAccountContact(body)
		if err != nil || !matchesAccountContact(accountContact, input) {
			return AccountContactAssociation{}, "uncertain"
		}
		return accountContact, "found"
	}
	var wire struct {
		AccountContacts []accountContactWire `json:"accountContacts"`
		Meta            struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.AccountContacts == nil {
		return AccountContactAssociation{}, "uncertain"
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total > len(wire.AccountContacts) {
		return AccountContactAssociation{}, "uncertain"
	}
	var match AccountContactAssociation
	for _, item := range wire.AccountContacts {
		accountContact, err := accountContactAssociation(item)
		if err != nil {
			return AccountContactAssociation{}, "uncertain"
		}
		if accountContact.ContactID == input.ContactID {
			if !matchesAccountContact(accountContact, input) {
				return AccountContactAssociation{}, "uncertain"
			}
			if match.ID != "" {
				return AccountContactAssociation{}, "uncertain"
			}
			match = accountContact
		}
	}
	if match.ID != "" {
		return match, "found"
	}
	return AccountContactAssociation{}, "absent"
}

func matchesAccountContact(link AccountContactAssociation, input CreateAccountContactInput) bool {
	return link.AccountID == input.AccountID && link.ContactID == input.ContactID && link.JobTitle == input.JobTitle
}

func (dispatch *accountContactCreateDispatch) execute(ctx context.Context) error {
	dispatch.response, dispatch.status, dispatch.retryAfter, dispatch.err = dispatch.client.requestWithContext(ctx, dispatch.call, http.MethodPost, "/accountContacts", dispatch.body)
	return nil
}
func accountContactCreateUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[AccountContactAssociation] {
	return sdkgo.NewMutationUncertain(AccountContactAssociation{}, queryFailure("createAccountContact", sdkgo.FailureTransport, message), dealReceipt(call, ""))
}
