// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

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
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// GetListInput selects one subscriber list by ID.
type GetListInput struct {
	// ListID is returned by ListSubscriberLists.
	ListID string `json:"listId"`
}

// ListProfile contains one list's basic settings.
type ListProfile struct {
	// ID is the opaque list ID.
	ID string `json:"id"`
	// Title is the list display name.
	Title string `json:"title"`
	// UnsubscribeSetting controls list-wide unsubscribe behavior.
	UnsubscribeSetting string `json:"unsubscribeSetting"`
	// ConfirmedOptIn reports whether subscriber confirmation is required.
	ConfirmedOptIn bool `json:"confirmedOptIn"`
	// UnsubscribePage is the configured unsubscribe landing page.
	UnsubscribePage string `json:"unsubscribePage,omitempty"`
	// ConfirmationSuccessPage is the configured confirmation landing page.
	ConfirmationSuccessPage string `json:"confirmationSuccessPage,omitempty"`
}

// CreateListInput contains the basic settings of a new list.
type CreateListInput struct {
	// ClientID is returned by ListClients.
	ClientID string `json:"clientId"`
	// Title is unique among the client's lists.
	Title string `json:"title"`
	// UnsubscribeSetting is AllClientLists or OnlyThisList.
	UnsubscribeSetting string `json:"unsubscribeSetting"`
	// ConfirmedOptIn enables double opt-in.
	ConfirmedOptIn bool `json:"confirmedOptIn"`
	// UnsubscribePage is an optional HTTPS landing page.
	UnsubscribePage string `json:"unsubscribePage,omitempty"`
	// ConfirmationSuccessPage is an optional HTTPS confirmation page.
	ConfirmationSuccessPage string `json:"confirmationSuccessPage,omitempty"`
}

// UpdateListInput replaces one list's basic settings.
type UpdateListInput struct {
	// ListID is returned by ListSubscriberLists.
	ListID string `json:"listId"`
	// Title is the new nonempty list title.
	Title string `json:"title"`
	// UnsubscribeSetting is AllClientLists or OnlyThisList.
	UnsubscribeSetting string `json:"unsubscribeSetting"`
	// ConfirmedOptIn enables double opt-in.
	ConfirmedOptIn bool `json:"confirmedOptIn"`
	// UnsubscribePage is an optional HTTPS landing page.
	UnsubscribePage string `json:"unsubscribePage,omitempty"`
	// ConfirmationSuccessPage is an optional HTTPS confirmation page.
	ConfirmationSuccessPage string `json:"confirmationSuccessPage,omitempty"`
}

// DeleteListInput selects a list for deletion.
type DeleteListInput struct {
	// ListID is returned by ListSubscriberLists.
	ListID string `json:"listId"`
}

// ListIdentity identifies the list changed by a Mutation.
type ListIdentity struct {
	// ID is the opaque list ID.
	ID string `json:"id"`
	// Title is known for create and update operations; blank for deletion.
	Title string `json:"title"`
}

// GetListOperation reads one list's settings.
type GetListOperation struct{ client *Client }

// CreateListOperation creates one subscriber list.
type CreateListOperation struct{ client *Client }

// UpdateListOperation replaces one list's settings.
type UpdateListOperation struct{ client *Client }

// DeleteListOperation deletes one subscriber list.
type DeleteListOperation struct{ client *Client }

// GetList binds list lookup to this client.
func (client *Client) GetList() GetListOperation { return GetListOperation{client} }

// CreateList binds list creation to this client.
func (client *Client) CreateList() CreateListOperation { return CreateListOperation{client} }

// UpdateList binds list replacement to this client.
func (client *Client) UpdateList() UpdateListOperation { return UpdateListOperation{client} }

// DeleteList binds list deletion to this client.
func (client *Client) DeleteList() DeleteListOperation { return DeleteListOperation{client} }

// Definition returns the generated list lookup contract.
func (GetListOperation) Definition() sdkgo.QueryDefinition { return GetListDefinition }

// Definition returns the generated list creation contract.
func (CreateListOperation) Definition() sdkgo.MutationDefinition { return CreateListDefinition }

// Definition returns the generated list replacement contract.
func (UpdateListOperation) Definition() sdkgo.MutationDefinition { return UpdateListDefinition }

// Definition returns the generated list deletion contract.
func (DeleteListOperation) Definition() sdkgo.MutationDefinition { return DeleteListDefinition }

// IdempotencyKey preserves identity across bounded list creation attempts.
func (CreateListOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across list replacements.
func (UpdateListOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across list deletion attempts.
func (DeleteListOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads a list's basic settings.
func (operation GetListOperation) Invoke(call sdkgo.Call, input GetListInput) sdkgo.QueryAttempt[ListProfile] {
	const name = "getList"
	if !validCampaignMonitorID(input.ListID) {
		return queryBranch(name, GetListBranchDefect, ListProfile{}, sdkgo.FailureValidation, "list ID is invalid")
	}
	return invokeQuery(call, operation.client, name, listPath(input.ListID), queryBranches{found: GetListBranchFound, notFound: GetListBranchNotFound, rejected: GetListBranchProviderRejected, invalid: GetListBranchInvalidResponse, defect: GetListBranchDefect}, func(body []byte) (ListProfile, error) {
		var wire struct {
			ID                      string `json:"ListID"`
			Title                   string `json:"Title"`
			UnsubscribeSetting      string `json:"UnsubscribeSetting"`
			ConfirmedOptIn          bool   `json:"ConfirmedOptIn"`
			UnsubscribePage         string `json:"UnsubscribePage"`
			ConfirmationSuccessPage string `json:"ConfirmationSuccessPage"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.ID != input.ListID || wire.Title == "" || !validUnsubscribeSetting(wire.UnsubscribeSetting) {
			return ListProfile{}, errors.New("list settings are invalid")
		}
		return ListProfile{ID: wire.ID, Title: wire.Title, UnsubscribeSetting: wire.UnsubscribeSetting, ConfirmedOptIn: wire.ConfirmedOptIn, UnsubscribePage: wire.UnsubscribePage, ConfirmationSuccessPage: wire.ConfirmationSuccessPage}, nil
	})
}

// Invoke creates one list and reads the client's lists after an unknown send.
func (operation CreateListOperation) Invoke(call sdkgo.Call, input CreateListInput) sdkgo.MutationAttempt[ListIdentity] {
	const name = "createList"
	if !validCampaignMonitorID(input.ClientID) || !validListSettings(input.Title, input.UnsubscribeSetting, input.UnsubscribePage, input.ConfirmationSuccessPage) {
		return listMutationBranch(name, CreateListBranchDefect, ListIdentity{}, sdkgo.FailureValidation, "client ID or list settings are invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateListDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return listMutationBranch(name, CreateListBranchDefect, ListIdentity{}, sdkgo.FailureLocalDefect, "list dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		if listID := operation.findCreatedList(call, input.ClientID, input.Title); listID != "" {
			return sdkgo.NewMutationBranch(CreateListBranchCreated, ListIdentity{ID: listID, Title: input.Title}, nil, sdkgo.Receipt{})
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(ListIdentity{}, queryFailure(name, sdkgo.FailureTransport, "an earlier list may have been created"), sdkgo.Receipt{})
	}
	payload := listSettingsPayload(input.Title, input.UnsubscribeSetting, input.ConfirmedOptIn, input.UnsubscribePage, input.ConfirmationSuccessPage)
	var body []byte
	var status int
	var retryAfter time.Duration
	var requestErr error
	err = fence.Dispatch(func(ctx context.Context) error {
		body, status, retryAfter, requestErr = operation.client.requestWithContext(ctx, call, http.MethodPost, "/lists/"+url.PathEscape(input.ClientID)+".json", payload)
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ListIdentity](queryFailure(name, sdkgo.FailureAvailability, "list dispatch was not recorded or sent"), 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[ListIdentity](queryFailure(name, sdkgo.FailureAvailability, "list retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[ListIdentity](queryFailure(name, kind, "Campaign Monitor asked to retry list creation"), retryAfter)
	}
	if err != nil || status == http.StatusRequestTimeout || status >= 500 {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(ListIdentity{}, queryFailure(name, sdkgo.FailureTransport, "list outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[ListIdentity](queryFailure(name, sdkgo.FailureTransport, "list outcome is unknown"), 0)
	}
	if status != http.StatusCreated {
		return listMutationBranch(name, CreateListBranchProviderRejected, ListIdentity{}, sdkgo.FailureProviderRejection, "Campaign Monitor rejected HTTP "+strconv.Itoa(status))
	}
	var listID string
	if json.Unmarshal(body, &listID) != nil || !validCampaignMonitorID(listID) {
		return sdkgo.NewMutationUncertain(ListIdentity{}, queryFailure(name, sdkgo.FailureProtocol, "Campaign Monitor created a list but its ID is unknown"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateListBranchCreated, ListIdentity{ID: listID, Title: input.Title}, nil, sdkgo.Receipt{})
}

func (operation CreateListOperation) findCreatedList(call sdkgo.Call, clientID, title string) string {
	body, status, _, err := operation.client.get(call, "/clients/"+url.PathEscape(clientID)+"/lists.json")
	if err != nil || status != http.StatusOK {
		return ""
	}
	var rows []struct {
		ID   string `json:"ListID"`
		Name string `json:"Name"`
	}
	if json.Unmarshal(body, &rows) != nil {
		return ""
	}
	var found string
	for _, row := range rows {
		if row.Name == title {
			if found != "" || !validCampaignMonitorID(row.ID) {
				return ""
			}
			found = row.ID
		}
	}
	return found
}

// Invoke replaces list settings without suppression-list scrubbing.
func (operation UpdateListOperation) Invoke(call sdkgo.Call, input UpdateListInput) sdkgo.MutationAttempt[ListIdentity] {
	const name = "updateList"
	if !validCampaignMonitorID(input.ListID) || !validListSettings(input.Title, input.UnsubscribeSetting, input.UnsubscribePage, input.ConfirmationSuccessPage) {
		return listMutationBranch(name, UpdateListBranchDefect, ListIdentity{}, sdkgo.FailureValidation, "list ID or settings are invalid")
	}
	payload := listSettingsPayload(input.Title, input.UnsubscribeSetting, input.ConfirmedOptIn, input.UnsubscribePage, input.ConfirmationSuccessPage)
	payload["AddUnsubscribesToSuppList"] = false
	payload["ScrubActiveWithSuppList"] = false
	return invokeListWrite(call, operation.client, name, http.MethodPut, listPath(input.ListID), payload, ListIdentity{ID: input.ListID, Title: input.Title}, UpdateListBranchUpdated, UpdateListBranchNotFound, UpdateListBranchProviderRejected, UpdateListBranchInvalidResponse, UpdateListBranchDefect)
}

// Invoke deletes one list; Campaign Monitor rejects lists attached to campaign drafts.
func (operation DeleteListOperation) Invoke(call sdkgo.Call, input DeleteListInput) sdkgo.MutationAttempt[ListIdentity] {
	const name = "deleteList"
	if !validCampaignMonitorID(input.ListID) {
		return listMutationBranch(name, DeleteListBranchDefect, ListIdentity{}, sdkgo.FailureValidation, "list ID is invalid")
	}
	return invokeListWrite(call, operation.client, name, http.MethodDelete, listPath(input.ListID), nil, ListIdentity{ID: input.ListID}, DeleteListBranchDeleted, "", DeleteListBranchProviderRejected, DeleteListBranchInvalidResponse, DeleteListBranchDefect)
}

func invokeListWrite(call sdkgo.Call, client *Client, name, method, path string, payload any, identity ListIdentity, success, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[ListIdentity] {
	body, status, retryAfter, err := client.request(call, method, path, payload)
	switch {
	case errors.Is(err, errCredential):
		return listMutationBranch(name, defect, ListIdentity{}, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	case errors.Is(err, errRequest):
		return sdkgo.NewMutationRetry[ListIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor request failed"), 0)
	case status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[ListIdentity](queryFailure(name, sdkgo.FailureRateLimit, "Campaign Monitor rate limited the request"), retryAfter)
	case status == http.StatusRequestTimeout || status >= 500:
		return sdkgo.NewMutationRetry[ListIdentity](queryFailure(name, sdkgo.FailureAvailability, "Campaign Monitor is temporarily unavailable"), retryAfter)
	case status == http.StatusNotFound && notFound != "":
		return listMutationBranch(name, notFound, ListIdentity{}, sdkgo.FailureNotFound, "list was not found")
	case status != http.StatusOK && status != http.StatusNoContent:
		return listMutationBranch(name, rejected, ListIdentity{}, sdkgo.FailureProviderRejection, "Campaign Monitor rejected HTTP "+strconv.Itoa(status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return listMutationBranch(name, invalid, ListIdentity{}, sdkgo.FailureResponseTooLarge, "Campaign Monitor response exceeded the limit")
	case err != nil:
		return sdkgo.NewMutationRetry[ListIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor response was interrupted"), 0)
	case len(body) != 0:
		return listMutationBranch(name, invalid, ListIdentity{}, sdkgo.FailureProtocol, "Campaign Monitor returned an unexpected response body")
	}
	return sdkgo.NewMutationBranch(success, identity, nil, sdkgo.Receipt{})
}

func listPath(listID string) string { return "/lists/" + url.PathEscape(listID) + ".json" }

func validUnsubscribeSetting(setting string) bool {
	return setting == "AllClientLists" || setting == "OnlyThisList"
}

func validListSettings(title, setting, unsubscribePage, confirmationPage string) bool {
	return strings.TrimSpace(title) != "" && title == strings.TrimSpace(title) && validUnsubscribeSetting(setting) && validOptionalListURL(unsubscribePage) && validOptionalListURL(confirmationPage)
}

func validOptionalListURL(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func listSettingsPayload(title, setting string, confirmed bool, unsubscribePage, confirmationPage string) map[string]any {
	payload := map[string]any{"Title": title, "UnsubscribeSetting": setting, "ConfirmedOptIn": confirmed}
	if unsubscribePage != "" {
		payload["UnsubscribePage"] = unsubscribePage
	}
	if confirmationPage != "" {
		payload["ConfirmationSuccessPage"] = confirmationPage
	}
	return payload
}

func listMutationBranch(name string, branch sdkgo.BranchID, value ListIdentity, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ListIdentity] {
	failure := queryFailure(name, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
