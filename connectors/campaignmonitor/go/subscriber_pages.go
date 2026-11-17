// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListSubscribersInput selects a page of subscribers in one list state.
type ListSubscribersInput struct {
	// ListID is an opaque ID from ListSubscriberLists.
	ListID string `json:"listId"`
	// State is active, unconfirmed, unsubscribed, bounced, or deleted; blank uses active.
	State string `json:"state,omitempty"`
	// PageNumber starts at 1; zero uses the first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 to 1000; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
}

// SubscribersPage contains one page and its pagination counts.
type SubscribersPage struct {
	// Subscribers are non-secret subscriber identities in this page.
	Subscribers []SubscriberProfile `json:"subscribers"`
	// PageNumber is the returned one-based page number.
	PageNumber int `json:"pageNumber"`
	// PageSize is the returned requested page size.
	PageSize int `json:"pageSize"`
	// TotalRecords is the count across pages.
	TotalRecords int `json:"totalRecords"`
	// NumberOfPages is the available page count.
	NumberOfPages int `json:"numberOfPages"`
}

// ListSubscribersOperation reads a page of subscribers.
type ListSubscribersOperation struct{ client *Client }

// ListSubscribers binds subscriber page lookup to this client.
func (client *Client) ListSubscribers() ListSubscribersOperation {
	return ListSubscribersOperation{client: client}
}

// Definition returns the generated subscriber page contract.
func (ListSubscribersOperation) Definition() sdkgo.QueryDefinition { return ListSubscribersDefinition }

// Invoke reads a page without mobile numbers or custom fields.
func (operation ListSubscribersOperation) Invoke(call sdkgo.Call, input ListSubscribersInput) sdkgo.QueryAttempt[SubscribersPage] {
	const name = "listSubscribers"
	if !validCampaignMonitorID(input.ListID) || input.PageNumber < 0 || input.PageSize < 0 || input.PageSize > 1000 {
		return queryBranch(name, ListSubscribersBranchDefect, SubscribersPage{}, sdkgo.FailureValidation, "list ID, page number, or page size is invalid")
	}
	state := input.State
	if state == "" {
		state = "active"
	}
	switch state {
	case "active", "unconfirmed", "unsubscribed", "bounced", "deleted":
	default:
		return queryBranch(name, ListSubscribersBranchDefect, SubscribersPage{}, sdkgo.FailureValidation, "subscriber state is invalid")
	}
	if input.PageNumber == 0 {
		input.PageNumber = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	path := "/lists/" + url.PathEscape(input.ListID) + "/" + state + ".json?page=" + strconv.Itoa(input.PageNumber) + "&pagesize=" + strconv.Itoa(input.PageSize)
	return invokeQuery(call, operation.client, name, path, queryBranches{found: ListSubscribersBranchListed, rejected: ListSubscribersBranchProviderRejected, invalid: ListSubscribersBranchInvalidResponse, defect: ListSubscribersBranchDefect}, func(body []byte) (SubscribersPage, error) {
		var wire struct {
			Results []struct {
				Email string `json:"EmailAddress"`
				Name  string `json:"Name"`
				State string `json:"State"`
			} `json:"Results"`
			PageNumber    int `json:"PageNumber"`
			PageSize      int `json:"PageSize"`
			TotalRecords  int `json:"TotalNumberOfRecords"`
			NumberOfPages int `json:"NumberOfPages"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Results == nil || wire.PageNumber != input.PageNumber || wire.PageSize != input.PageSize || wire.TotalRecords < 0 || wire.NumberOfPages < 0 {
			return SubscribersPage{}, errors.New("subscriber page is invalid")
		}
		page := SubscribersPage{Subscribers: make([]SubscriberProfile, 0, len(wire.Results)), PageNumber: wire.PageNumber, PageSize: wire.PageSize, TotalRecords: wire.TotalRecords, NumberOfPages: wire.NumberOfPages}
		for _, row := range wire.Results {
			if !validSubscriberEmail(row.Email) || row.State == "" {
				return SubscribersPage{}, errors.New("subscriber identity is invalid")
			}
			page.Subscribers = append(page.Subscribers, SubscriberProfile{Email: row.Email, Name: row.Name, State: row.State})
		}
		return page, nil
	})
}
