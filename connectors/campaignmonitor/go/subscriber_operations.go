// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListSubscriberListsInput selects one client returned by ListClients.
type ListSubscriberListsInput struct {
	// ClientID is an opaque ID from ListClients; blank is invalid.
	ClientID string `json:"clientId"`
}

// SubscriberListIdentity contains a list ID and display name.
type SubscriberListIdentity struct {
	// ID is the opaque list ID for subscriber operations.
	ID string `json:"id"`
	// Name is the provider's list name.
	Name string `json:"name"`
}

// SubscriberLists contains the lists owned by a client.
type SubscriberLists struct {
	// Lists contains all lists visible for the client.
	Lists []SubscriberListIdentity `json:"lists"`
}

// GetSubscriberInput selects one subscriber in a list.
type GetSubscriberInput struct {
	// ListID is an opaque ID from ListSubscriberLists.
	ListID string `json:"listId"`
	// Email is the complete subscriber email address.
	Email string `json:"email"`
}

// SubscriberProfile contains a subscriber's non-secret identity and state.
type SubscriberProfile struct {
	// Email is the subscriber's email address.
	Email string `json:"email"`
	// Name is the subscriber's display name.
	Name string `json:"name"`
	// State is the provider state, such as Active or Deleted.
	State string `json:"state"`
}

// UpsertSubscriberInput adds or updates one email address on a list.
type UpsertSubscriberInput struct {
	// ListID is an opaque ID from ListSubscriberLists.
	ListID string `json:"listId"`
	// Email is the complete subscriber email address.
	Email string `json:"email"`
	// Name is the nonblank subscriber display name.
	Name string `json:"name"`
}

// UpdateSubscriberInput replaces an existing subscriber's name.
type UpdateSubscriberInput struct {
	// ListID is an opaque ID from ListSubscriberLists.
	ListID string `json:"listId"`
	// Email is the current complete subscriber email address.
	Email string `json:"email"`
	// Name replaces the subscriber's current display name.
	Name string `json:"name"`
}

// DeleteSubscriberInput marks a subscriber deleted on one list.
type DeleteSubscriberInput struct {
	// ListID is an opaque ID from ListSubscriberLists.
	ListID string `json:"listId"`
	// Email is the complete subscriber email address.
	Email string `json:"email"`
}

// UnsubscribeSubscriberInput removes one address from campaign delivery.
type UnsubscribeSubscriberInput struct {
	// ListID is an opaque ID from ListSubscriberLists.
	ListID string `json:"listId"`
	// Email is the complete subscriber email address.
	Email string `json:"email"`
}

// SubscriberIdentity identifies the email address changed by a Mutation.
type SubscriberIdentity struct {
	// Email is the subscriber email address used for the write.
	Email string `json:"email"`
}

// ListSubscriberListsOperation reads a client's lists.
type ListSubscriberListsOperation struct{ client *Client }

// GetSubscriberOperation reads one subscriber.
type GetSubscriberOperation struct{ client *Client }

// UpsertSubscriberOperation adds or updates a subscriber.
type UpsertSubscriberOperation struct{ client *Client }

// UpdateSubscriberOperation updates an existing subscriber.
type UpdateSubscriberOperation struct{ client *Client }

// DeleteSubscriberOperation marks a subscriber deleted.
type DeleteSubscriberOperation struct{ client *Client }

// UnsubscribeSubscriberOperation marks a subscriber unsubscribed.
type UnsubscribeSubscriberOperation struct{ client *Client }

// ListSubscriberLists binds list discovery to this client.
func (client *Client) ListSubscriberLists() ListSubscriberListsOperation {
	return ListSubscriberListsOperation{client}
}

// GetSubscriber binds subscriber lookup to this client.
func (client *Client) GetSubscriber() GetSubscriberOperation { return GetSubscriberOperation{client} }

// UpsertSubscriber binds subscriber upsert to this client.
func (client *Client) UpsertSubscriber() UpsertSubscriberOperation {
	return UpsertSubscriberOperation{client}
}

// UpdateSubscriber binds subscriber name replacement to this client.
func (client *Client) UpdateSubscriber() UpdateSubscriberOperation {
	return UpdateSubscriberOperation{client}
}

// DeleteSubscriber binds subscriber deletion to this client.
func (client *Client) DeleteSubscriber() DeleteSubscriberOperation {
	return DeleteSubscriberOperation{client}
}

// UnsubscribeSubscriber binds unsubscribe delivery to this client.
func (client *Client) UnsubscribeSubscriber() UnsubscribeSubscriberOperation {
	return UnsubscribeSubscriberOperation{client}
}

// Definition returns the generated list discovery contract.
func (ListSubscriberListsOperation) Definition() sdkgo.QueryDefinition {
	return ListSubscriberListsDefinition
}

// Definition returns the generated subscriber lookup contract.
func (GetSubscriberOperation) Definition() sdkgo.QueryDefinition { return GetSubscriberDefinition }

// Definition returns the generated subscriber upsert contract.
func (UpsertSubscriberOperation) Definition() sdkgo.MutationDefinition {
	return UpsertSubscriberDefinition
}

// Definition returns the generated subscriber update contract.
func (UpdateSubscriberOperation) Definition() sdkgo.MutationDefinition {
	return UpdateSubscriberDefinition
}

// Definition returns the generated subscriber deletion contract.
func (DeleteSubscriberOperation) Definition() sdkgo.MutationDefinition {
	return DeleteSubscriberDefinition
}

// Definition returns the generated unsubscribe contract.
func (UnsubscribeSubscriberOperation) Definition() sdkgo.MutationDefinition {
	return UnsubscribeSubscriberDefinition
}

// IdempotencyKey derives a stable identity for the email upsert.
func (UpsertSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable identity for the absolute name replacement.
func (UpdateSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable identity for the deletion.
func (DeleteSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable identity for the unsubscribe.
func (UnsubscribeSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ UnsubscribeSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists subscriber lists for one client.
func (operation ListSubscriberListsOperation) Invoke(call sdkgo.Call, input ListSubscriberListsInput) sdkgo.QueryAttempt[SubscriberLists] {
	const name = "listSubscriberLists"
	if !validCampaignMonitorID(input.ClientID) {
		return queryBranch(name, "defect", SubscriberLists{}, sdkgo.FailureValidation, "client ID is invalid")
	}
	return invokeQuery(call, operation.client, name, "/clients/"+url.PathEscape(input.ClientID)+"/lists.json", queryBranches{found: "listed", rejected: "providerRejected", invalid: "invalidResponse", defect: "defect"}, func(body []byte) (SubscriberLists, error) {
		var rows []struct {
			ID   string `json:"ListID"`
			Name string `json:"Name"`
		}
		if err := json.Unmarshal(body, &rows); err != nil || rows == nil {
			return SubscriberLists{}, errors.New("list JSON missing")
		}
		lists := make([]SubscriberListIdentity, 0, len(rows))
		for _, row := range rows {
			if row.ID == "" || row.Name == "" {
				return SubscriberLists{}, errors.New("list identity missing")
			}
			lists = append(lists, SubscriberListIdentity{ID: row.ID, Name: row.Name})
		}
		return SubscriberLists{Lists: lists}, nil
	})
}

// Invoke reads one subscriber without returning custom fields or mobile number.
func (operation GetSubscriberOperation) Invoke(call sdkgo.Call, input GetSubscriberInput) sdkgo.QueryAttempt[SubscriberProfile] {
	const name = "getSubscriber"
	if !validCampaignMonitorID(input.ListID) || !validSubscriberEmail(input.Email) {
		return queryBranch(name, "defect", SubscriberProfile{}, sdkgo.FailureValidation, "list ID or email is invalid")
	}
	path := subscriberPath(input.ListID) + "?email=" + url.QueryEscape(input.Email)
	return invokeQuery(call, operation.client, name, path, queryBranches{found: "found", notFound: "notFound", rejected: "providerRejected", invalid: "invalidResponse", defect: "defect"}, func(body []byte) (SubscriberProfile, error) {
		var row struct {
			Email string `json:"EmailAddress"`
			Name  string `json:"Name"`
			State string `json:"State"`
		}
		if err := json.Unmarshal(body, &row); err != nil || row.Email == "" || row.State == "" {
			return SubscriberProfile{}, errors.New("subscriber identity missing")
		}
		return SubscriberProfile{Email: row.Email, Name: row.Name, State: row.State}, nil
	})
}

// Invoke queues an add or update with resubscription and autoresponders disabled.
func (operation UpsertSubscriberOperation) Invoke(call sdkgo.Call, input UpsertSubscriberInput) sdkgo.MutationAttempt[SubscriberIdentity] {
	const name = "upsertSubscriber"
	if !validCampaignMonitorID(input.ListID) || !validSubscriberEmail(input.Email) || strings.TrimSpace(input.Name) == "" {
		return campaignMonitorMutationBranch(name, "defect", SubscriberIdentity{}, sdkgo.FailureValidation, "list ID, email, and name are required")
	}
	payload := map[string]any{"EmailAddress": input.Email, "Name": input.Name, "Resubscribe": false, "RestartSubscriptionBasedAutoresponders": false}
	return invokeSubscriberWrite(call, operation.client, name, http.MethodPost, subscriberPath(input.ListID), payload, http.StatusCreated, input.Email, "saved", "", subscriberWriteOptions{})
}

// Invoke replaces the name of an existing subscriber without resubscribing.
func (operation UpdateSubscriberOperation) Invoke(call sdkgo.Call, input UpdateSubscriberInput) sdkgo.MutationAttempt[SubscriberIdentity] {
	const name = "updateSubscriber"
	if !validCampaignMonitorID(input.ListID) || !validSubscriberEmail(input.Email) || strings.TrimSpace(input.Name) == "" {
		return campaignMonitorMutationBranch(name, "defect", SubscriberIdentity{}, sdkgo.FailureValidation, "list ID, email, and name are required")
	}
	payload := map[string]any{"EmailAddress": input.Email, "Name": input.Name, "Resubscribe": false, "RestartSubscriptionBasedAutoresponders": false}
	return invokeSubscriberWrite(call, operation.client, name, http.MethodPut, subscriberPath(input.ListID)+"?email="+url.QueryEscape(input.Email), payload, http.StatusOK, input.Email, "updated", "notFound", subscriberWriteOptions{})
}

// Invoke marks an active subscriber deleted without adding them to suppression.
func (operation DeleteSubscriberOperation) Invoke(call sdkgo.Call, input DeleteSubscriberInput) sdkgo.MutationAttempt[SubscriberIdentity] {
	const name = "deleteSubscriber"
	if !validCampaignMonitorID(input.ListID) || !validSubscriberEmail(input.Email) {
		return campaignMonitorMutationBranch(name, "defect", SubscriberIdentity{}, sdkgo.FailureValidation, "list ID or email is invalid")
	}
	return invokeSubscriberWrite(call, operation.client, name, http.MethodDelete, subscriberPath(input.ListID)+"?email="+url.QueryEscape(input.Email), nil, http.StatusOK, input.Email, "deleted", "", subscriberWriteOptions{})
}

// Invoke unsubscribes one address, allowing the list's suppression policy to apply.
func (operation UnsubscribeSubscriberOperation) Invoke(call sdkgo.Call, input UnsubscribeSubscriberInput) sdkgo.MutationAttempt[SubscriberIdentity] {
	const name = "unsubscribeSubscriber"
	if !validCampaignMonitorID(input.ListID) || !validSubscriberEmail(input.Email) {
		return campaignMonitorMutationBranch(name, "defect", SubscriberIdentity{}, sdkgo.FailureValidation, "list ID or email is invalid")
	}
	path := "/subscribers/" + url.PathEscape(input.ListID) + "/unsubscribe.json"
	payload := map[string]string{"EmailAddress": input.Email}
	readBackPath := subscriberPath(input.ListID) + "?email=" + url.QueryEscape(input.Email)
	return invokeSubscriberWrite(call, operation.client, name, http.MethodPost, path, payload, http.StatusOK, input.Email, "unsubscribed", "", subscriberWriteOptions{emptyResponse: true, confirmUnsubscribedPath: readBackPath})
}

func subscriberPath(listID string) string { return "/subscribers/" + url.PathEscape(listID) + ".json" }
func validCampaignMonitorID(id string) bool {
	return id != "" && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "/?#\r\n")
}
func validSubscriberEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && strings.TrimSpace(email) == email
}

type subscriberWriteOptions struct {
	emptyResponse            bool
	confirmUnsubscribedPath string
}

func invokeSubscriberWrite(call sdkgo.Call, client *Client, name, method, path string, payload any, successStatus int, email string, success, notFound sdkgo.BranchID, options subscriberWriteOptions) sdkgo.MutationAttempt[SubscriberIdentity] {
	zero := SubscriberIdentity{}
	body, status, retryAfter, err := client.request(call, method, path, payload)
	switch {
	case errors.Is(err, errCredential):
		return campaignMonitorMutationBranch(name, "defect", zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	case errors.Is(err, errRequest):
		return sdkgo.NewMutationRetry[SubscriberIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor request failed"), 0)
	case status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[SubscriberIdentity](queryFailure(name, sdkgo.FailureRateLimit, "Campaign Monitor rate limited the request"), retryAfter)
	case status == http.StatusRequestTimeout || status >= 500:
		return sdkgo.NewMutationRetry[SubscriberIdentity](queryFailure(name, sdkgo.FailureAvailability, "Campaign Monitor is temporarily unavailable"), retryAfter)
	case status == http.StatusNotFound && notFound != "":
		return campaignMonitorMutationBranch(name, notFound, zero, sdkgo.FailureNotFound, "subscriber was not found")
	case status == http.StatusBadRequest && options.confirmUnsubscribedPath != "" && isSubscriberNotInList(body):
		return confirmSubscriberUnsubscribed(call, client, name, options.confirmUnsubscribedPath, email)
	case status != successStatus && !(method == http.MethodDelete && status == http.StatusNoContent):
		return campaignMonitorMutationBranch(name, "providerRejected", zero, sdkgo.FailureProviderRejection, "Campaign Monitor rejected HTTP "+strconv.Itoa(status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return campaignMonitorMutationBranch(name, "invalidResponse", zero, sdkgo.FailureResponseTooLarge, "Campaign Monitor response exceeded the configured limit")
	case err != nil:
		return sdkgo.NewMutationRetry[SubscriberIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor response was interrupted"), time.Duration(0))
	}
	if options.emptyResponse {
		if len(body) != 0 {
			return campaignMonitorMutationBranch(name, "invalidResponse", zero, sdkgo.FailureProtocol, "Campaign Monitor returned an unexpected unsubscribe body")
		}
	} else if method != http.MethodDelete {
		var returnedEmail string
		if json.Unmarshal(body, &returnedEmail) != nil || !strings.EqualFold(returnedEmail, email) {
			return campaignMonitorMutationBranch(name, "invalidResponse", zero, sdkgo.FailureProtocol, "Campaign Monitor returned invalid subscriber identity")
		}
	} else if len(body) != 0 {
		return campaignMonitorMutationBranch(name, "invalidResponse", zero, sdkgo.FailureProtocol, "Campaign Monitor returned an unexpected deletion body")
	}
	return sdkgo.NewMutationBranch(success, SubscriberIdentity{Email: email}, nil, sdkgo.Receipt{})
}

func isSubscriberNotInList(body []byte) bool {
	var response struct {
		Code int `json:"Code"`
	}
	return json.Unmarshal(body, &response) == nil && response.Code == 203
}

func confirmSubscriberUnsubscribed(call sdkgo.Call, client *Client, name, path, email string) sdkgo.MutationAttempt[SubscriberIdentity] {
	zero := SubscriberIdentity{}
	body, status, retryAfter, err := client.request(call, http.MethodGet, path, nil)
	switch {
	case errors.Is(err, errCredential):
		return campaignMonitorMutationBranch(name, "defect", zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	case errors.Is(err, errRequest):
		return sdkgo.NewMutationRetry[SubscriberIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor status read failed"), 0)
	case status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[SubscriberIdentity](queryFailure(name, sdkgo.FailureRateLimit, "Campaign Monitor rate limited status read"), retryAfter)
	case status == http.StatusRequestTimeout || status >= 500:
		return sdkgo.NewMutationRetry[SubscriberIdentity](queryFailure(name, sdkgo.FailureAvailability, "Campaign Monitor status is temporarily unavailable"), retryAfter)
	case status != http.StatusOK:
		return campaignMonitorMutationBranch(name, "providerRejected", zero, sdkgo.FailureProviderRejection, "Campaign Monitor did not confirm the unsubscribe")
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return campaignMonitorMutationBranch(name, "invalidResponse", zero, sdkgo.FailureResponseTooLarge, "Campaign Monitor status response exceeded the configured limit")
	case err != nil:
		return sdkgo.NewMutationRetry[SubscriberIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor status response was interrupted"), 0)
	}
	var profile struct {
		Email string `json:"EmailAddress"`
		State string `json:"State"`
	}
	if json.Unmarshal(body, &profile) != nil || !strings.EqualFold(profile.Email, email) || profile.State == "" {
		return campaignMonitorMutationBranch(name, "invalidResponse", zero, sdkgo.FailureProtocol, "Campaign Monitor returned invalid subscriber status")
	}
	if profile.State != "Unsubscribed" {
		return campaignMonitorMutationBranch(name, "providerRejected", zero, sdkgo.FailureProviderRejection, "Campaign Monitor did not confirm the unsubscribe")
	}
	return sdkgo.NewMutationBranch[SubscriberIdentity]("unsubscribed", SubscriberIdentity{Email: email}, nil, sdkgo.Receipt{})
}

func campaignMonitorMutationBranch(name string, branch sdkgo.BranchID, value SubscriberIdentity, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SubscriberIdentity] {
	failure := queryFailure(name, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
