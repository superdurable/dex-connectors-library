// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListSubscribersInput selects a cursor page of up to 1000 subscribers.
type ListSubscribersInput struct {
	// After is the previous page's end cursor; blank starts from the first page.
	After string `json:"after,omitempty"`
	// PerPage is the maximum count; zero defaults to 20.
	PerPage int `json:"perPage,omitempty"`
}

// SubscriberMetadata contains subscriber identity without custom fields.
type SubscriberMetadata struct {
	// ID is Kit's stable numeric subscriber ID.
	ID int64 `json:"id"`
	// EmailAddress is the subscriber email address.
	EmailAddress string `json:"emailAddress"`
	// FirstName is the given name, when present.
	FirstName string `json:"firstName"`
	// State is Kit's current subscriber lifecycle state.
	State string `json:"state"`
}

// SubscribersPage contains one cursor page of subscriber metadata.
type SubscribersPage struct {
	// Subscribers contains the returned subscriber records.
	Subscribers []SubscriberMetadata `json:"subscribers"`
	// HasNextPage reports whether the end cursor can fetch another page.
	HasNextPage bool `json:"hasNextPage"`
	// NextCursor is the next page cursor when HasNextPage is true.
	NextCursor string `json:"nextCursor"`
	// PerPage is the requested maximum count.
	PerPage int `json:"perPage"`
}

// GetSubscriberInput selects one numeric Kit subscriber ID.
type GetSubscriberInput struct {
	// SubscriberID is the ID from a subscriber page or API.
	SubscriberID int64 `json:"subscriberId"`
}

// ListSubscribersOperation retrieves one page of subscriber metadata.
type ListSubscribersOperation struct{ client *Client }

// GetSubscriberOperation retrieves one subscriber's metadata.
type GetSubscriberOperation struct{ client *Client }

// ListSubscribers binds the list Query to this client.
func (client *Client) ListSubscribers() ListSubscribersOperation {
	return ListSubscribersOperation{client}
}

// GetSubscriber binds the subscriber Query to this client.
func (client *Client) GetSubscriber() GetSubscriberOperation { return GetSubscriberOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListSubscribersOperation) Definition() sdkgo.QueryDefinition { return ListSubscribersDefinition }

// Definition returns the manifest-generated subscriber contract.
func (GetSubscriberOperation) Definition() sdkgo.QueryDefinition { return GetSubscriberDefinition }

// Invoke reads one cursor page of subscriber identities and states.
func (operation ListSubscribersOperation) Invoke(call sdkgo.Call, input ListSubscribersInput) sdkgo.QueryAttempt[SubscribersPage] {
	branches := queryBranches{found: ListSubscribersBranchListed, rejected: ListSubscribersBranchProviderRejected, invalid: ListSubscribersBranchInvalidResponse, defect: ListSubscribersBranchDefect}
	if input.PerPage < 0 || input.PerPage > 1000 || len(input.After) > 2048 || !utf8.ValidString(input.After) {
		return queryBranch("listSubscribers", branches.defect, SubscribersPage{}, sdkgo.FailureValidation, "cursor or perPage is invalid")
	}
	if input.PerPage == 0 {
		input.PerPage = 20
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}, "slim": {"true"}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return invokeQuery(call, operation.client, "listSubscribers", "/subscribers?"+query.Encode(), branches, func(body []byte) (SubscribersPage, error) { return decodeSubscribersPage(body, input) })
}

// Invoke reads one subscriber without custom fields.
func (operation GetSubscriberOperation) Invoke(call sdkgo.Call, input GetSubscriberInput) sdkgo.QueryAttempt[SubscriberMetadata] {
	branches := queryBranches{found: GetSubscriberBranchFound, notFound: GetSubscriberBranchNotFound, rejected: GetSubscriberBranchProviderRejected, invalid: GetSubscriberBranchInvalidResponse, defect: GetSubscriberBranchDefect}
	if input.SubscriberID <= 0 {
		return queryBranch("getSubscriber", branches.defect, SubscriberMetadata{}, sdkgo.FailureValidation, "subscriber ID must be positive")
	}
	return invokeQuery(call, operation.client, "getSubscriber", "/subscribers/"+strconv.FormatInt(input.SubscriberID, 10), branches, decodeSubscriber)
}

type subscriberWire struct {
	ID           int64  `json:"id"`
	EmailAddress string `json:"email_address"`
	FirstName    string `json:"first_name"`
	State        string `json:"state"`
}

func decodeSubscribersPage(body []byte, input ListSubscribersInput) (SubscribersPage, error) {
	var wire struct {
		Subscribers []subscriberWire `json:"subscribers"`
		Pagination  *struct {
			HasNextPage bool   `json:"has_next_page"`
			EndCursor   string `json:"end_cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SubscribersPage{}, err
	}
	if wire.Subscribers == nil || wire.Pagination == nil {
		return SubscribersPage{}, fmt.Errorf("subscriber page missing")
	}
	if wire.Pagination.HasNextPage && wire.Pagination.EndCursor == "" {
		return SubscribersPage{}, fmt.Errorf("next cursor missing")
	}
	page := SubscribersPage{Subscribers: make([]SubscriberMetadata, 0, len(wire.Subscribers)), HasNextPage: wire.Pagination.HasNextPage, PerPage: input.PerPage}
	if wire.Pagination.HasNextPage {
		page.NextCursor = wire.Pagination.EndCursor
	}
	for _, subscriber := range wire.Subscribers {
		metadata, err := subscriberMetadata(subscriber)
		if err != nil {
			return SubscribersPage{}, err
		}
		page.Subscribers = append(page.Subscribers, metadata)
	}
	return page, nil
}

func decodeSubscriber(body []byte) (SubscriberMetadata, error) {
	var wire struct {
		Subscriber *subscriberWire `json:"subscriber"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SubscriberMetadata{}, err
	}
	if wire.Subscriber == nil {
		return SubscriberMetadata{}, fmt.Errorf("subscriber missing")
	}
	return subscriberMetadata(*wire.Subscriber)
}

func subscriberMetadata(subscriber subscriberWire) (SubscriberMetadata, error) {
	if subscriber.ID <= 0 || subscriber.EmailAddress == "" || subscriber.State == "" {
		return SubscriberMetadata{}, fmt.Errorf("subscriber identity missing or invalid")
	}
	return SubscriberMetadata{ID: subscriber.ID, EmailAddress: subscriber.EmailAddress, FirstName: subscriber.FirstName, State: subscriber.State}, nil
}
