// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drip

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListSubscribersInput selects a bounded Drip subscriber page.
type ListSubscribersInput struct {
	// AccountID is a numeric account ID returned by ListAccounts.
	AccountID string `json:"accountId"`
	// Page is one-based; zero selects the first page.
	Page int `json:"page,omitempty"`
	// PerPage is the number of subscribers, from 1 to 1000; zero selects 100.
	PerPage int `json:"perPage,omitempty"`
	// Status is all, active, unsubscribed, active_or_unsubscribed, or undeliverable; blank selects active.
	Status string `json:"status,omitempty"`
}

// SubscribersPage contains only identity and status from a Drip subscriber page.
type SubscribersPage struct {
	// Subscribers contains the page's subscriber metadata.
	Subscribers []SubscriberMetadata `json:"subscribers"`
	// Page is the one-based page returned by Drip.
	Page int `json:"page"`
	// TotalPages is the total number of pages reported by Drip.
	TotalPages int `json:"totalPages"`
	// TotalCount is the number of subscribers matching the filter.
	TotalCount int `json:"totalCount"`
}

// UpsertSubscriberInput creates or updates a subscriber identified by email.
type UpsertSubscriberInput struct {
	// AccountID is a numeric account ID returned by ListAccounts.
	AccountID string `json:"accountId"`
	// Email is the subscriber's email address and stable upsert identity.
	Email string `json:"email"`
	// FirstName replaces the first name when nonempty.
	FirstName string `json:"firstName,omitempty"`
	// LastName replaces the last name when nonempty.
	LastName string `json:"lastName,omitempty"`
}

// DeleteSubscriberInput selects one subscriber for permanent deletion.
type DeleteSubscriberInput struct {
	// AccountID is a numeric account ID returned by ListAccounts.
	AccountID string `json:"accountId"`
	// SubscriberIDOrEmail is the Drip subscriber ID or email address.
	SubscriberIDOrEmail string `json:"subscriberIdOrEmail"`
}

// DeletedSubscriber confirms that a subscriber is absent.
type DeletedSubscriber struct {
	// Deleted is true after Drip confirms deletion or absence.
	Deleted bool `json:"deleted"`
}

// ListSubscribersOperation reads a bounded page of Drip subscribers.
type ListSubscribersOperation struct{ client *Client }

// UpsertSubscriberOperation creates or updates one Drip subscriber.
type UpsertSubscriberOperation struct{ client *Client }

// DeleteSubscriberOperation deletes one Drip subscriber.
type DeleteSubscriberOperation struct{ client *Client }

// ListSubscribers binds the subscriber page Query to this client.
func (client *Client) ListSubscribers() ListSubscribersOperation {
	return ListSubscribersOperation{client}
}

// UpsertSubscriber binds the subscriber upsert Mutation to this client.
func (client *Client) UpsertSubscriber() UpsertSubscriberOperation {
	return UpsertSubscriberOperation{client}
}

// DeleteSubscriber binds the subscriber delete Mutation to this client.
func (client *Client) DeleteSubscriber() DeleteSubscriberOperation {
	return DeleteSubscriberOperation{client}
}

// Definition returns the generated subscriber page Query contract.
func (ListSubscribersOperation) Definition() sdkgo.QueryDefinition { return ListSubscribersDefinition }

// Definition returns the generated subscriber upsert Mutation contract.
func (UpsertSubscriberOperation) Definition() sdkgo.MutationDefinition {
	return UpsertSubscriberDefinition
}

// Definition returns the generated subscriber delete Mutation contract.
func (DeleteSubscriberOperation) Definition() sdkgo.MutationDefinition {
	return DeleteSubscriberDefinition
}

// IdempotencyKey derives a stable identity for retries of the upsert.
func (UpsertSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable identity for retries of the delete.
func (DeleteSubscriberOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSubscriberInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one bounded subscriber page without tracking or custom fields.
func (operation ListSubscribersOperation) Invoke(call sdkgo.Call, input ListSubscribersInput) sdkgo.QueryAttempt[SubscribersPage] {
	branches := queryBranches{found: ListSubscribersBranchListed, rejected: ListSubscribersBranchProviderRejected, invalid: ListSubscribersBranchInvalidResponse, defect: ListSubscribersBranchDefect}
	if !validAccountID(input.AccountID) || input.Page < 0 || input.PerPage < 0 || input.PerPage > 1000 || !validSubscriberStatus(input.Status) {
		return queryBranch("listSubscribers", branches.defect, SubscribersPage{}, sdkgo.FailureValidation, "account ID or subscriber page options are invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PerPage == 0 {
		input.PerPage = 100
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per_page": {strconv.Itoa(input.PerPage)}}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	return invokeQuery(call, operation.client, "listSubscribers", "/"+input.AccountID+"/subscribers?"+query.Encode(), branches, decodeSubscribersPage)
}

// Invoke creates or updates a subscriber with the same email and name on repeats.
func (operation UpsertSubscriberOperation) Invoke(call sdkgo.Call, input UpsertSubscriberInput) sdkgo.MutationAttempt[SubscriberMetadata] {
	branches := mutationBranches{success: UpsertSubscriberBranchUpserted, rejected: UpsertSubscriberBranchProviderRejected, invalid: UpsertSubscriberBranchInvalidResponse, defect: UpsertSubscriberBranchDefect}
	if !validAccountID(input.AccountID) || !validSubscriberEmail(input.Email) || !validName(input.FirstName) || !validName(input.LastName) {
		return mutationBranch("upsertSubscriber", branches.defect, SubscriberMetadata{}, sdkgo.FailureValidation, "account ID, email, or name is invalid")
	}
	body, err := json.Marshal(struct {
		Subscribers []struct {
			Email     string `json:"email"`
			FirstName string `json:"first_name,omitempty"`
			LastName  string `json:"last_name,omitempty"`
		} `json:"subscribers"`
	}{Subscribers: []struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name,omitempty"`
		LastName  string `json:"last_name,omitempty"`
	}{{Email: input.Email, FirstName: input.FirstName, LastName: input.LastName}}})
	if err != nil {
		return mutationBranch("upsertSubscriber", branches.defect, SubscriberMetadata{}, sdkgo.FailureValidation, "subscriber input could not be encoded")
	}
	return invokeMutation(call, operation.client, "upsertSubscriber", http.MethodPost, "/"+input.AccountID+"/subscribers", body, branches, decodeSubscriber)
}

// Invoke deletes a subscriber; a missing subscriber is already absent.
func (operation DeleteSubscriberOperation) Invoke(call sdkgo.Call, input DeleteSubscriberInput) sdkgo.MutationAttempt[DeletedSubscriber] {
	branches := mutationBranches{success: DeleteSubscriberBranchDeleted, rejected: DeleteSubscriberBranchProviderRejected, invalid: DeleteSubscriberBranchInvalidResponse, defect: DeleteSubscriberBranchDefect}
	if !validAccountID(input.AccountID) || !validSubscriberIdentifier(input.SubscriberIDOrEmail) {
		return mutationBranch("deleteSubscriber", branches.defect, DeletedSubscriber{}, sdkgo.FailureValidation, "account ID or subscriber identifier is invalid")
	}
	path := "/" + input.AccountID + "/subscribers/" + url.PathEscape(input.SubscriberIDOrEmail)
	return invokeMutation(call, operation.client, "deleteSubscriber", http.MethodDelete, path, nil, branches, func(body []byte) (DeletedSubscriber, error) {
		if len(body) != 0 {
			return DeletedSubscriber{}, fmt.Errorf("expected empty delete response")
		}
		return DeletedSubscriber{Deleted: true}, nil
	})
}

func decodeSubscribersPage(body []byte) (SubscribersPage, error) {
	var wire struct {
		Meta struct {
			Page       int `json:"page"`
			TotalPages int `json:"total_pages"`
			TotalCount int `json:"total_count"`
		} `json:"meta"`
		Subscribers []struct {
			ID        string `json:"id"`
			Email     string `json:"email"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Status    string `json:"status"`
		} `json:"subscribers"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SubscribersPage{}, err
	}
	if wire.Subscribers == nil || wire.Meta.Page < 1 || wire.Meta.TotalPages < 0 || wire.Meta.TotalCount < 0 {
		return SubscribersPage{}, fmt.Errorf("subscriber page missing")
	}
	page := SubscribersPage{Subscribers: make([]SubscriberMetadata, 0, len(wire.Subscribers)), Page: wire.Meta.Page, TotalPages: wire.Meta.TotalPages, TotalCount: wire.Meta.TotalCount}
	for _, subscriber := range wire.Subscribers {
		if subscriber.ID == "" || subscriber.Email == "" || subscriber.Status == "" {
			return SubscribersPage{}, fmt.Errorf("subscriber identity missing")
		}
		page.Subscribers = append(page.Subscribers, SubscriberMetadata{ID: subscriber.ID, Email: subscriber.Email, FirstName: subscriber.FirstName, LastName: subscriber.LastName, Status: subscriber.Status})
	}
	return page, nil
}

func validAccountID(accountID string) bool {
	value, err := strconv.ParseUint(accountID, 10, 64)
	return err == nil && value > 0
}
func validSubscriberStatus(status string) bool {
	return status == "" || status == "all" || status == "active" || status == "unsubscribed" || status == "active_or_unsubscribed" || status == "undeliverable"
}
func validSubscriberEmail(email string) bool {
	return validSubscriberIdentifier(email) && strings.Contains(email, "@")
}
func validSubscriberIdentifier(identifier string) bool {
	return identifier != "" && len(identifier) <= 512 && strings.TrimSpace(identifier) == identifier && utf8.ValidString(identifier)
}
func validName(name string) bool { return len(name) <= 512 && utf8.ValidString(name) }
