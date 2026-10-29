// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailgun

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetEventsInput selects one sending domain and a bounded events page.
type GetEventsInput struct {
	// Domain is a Mailgun sending domain such as example.test.
	Domain string `json:"domain"`
	// Limit is at most 300 events; zero uses Mailgun's default.
	Limit int `json:"limit,omitempty"`
}

// Event identifies one Mailgun delivery event.
type Event struct {
	// ID is the event's provider identifier.
	ID string `json:"id"`
	// Event is the event kind, such as delivered.
	Event string `json:"event"`
	// Timestamp is epoch seconds, including any fractional seconds.
	Timestamp float64 `json:"timestamp"`
}

// EventsOutput contains one domain events page.
type EventsOutput struct {
	// Events is the returned event page.
	Events []Event `json:"events"`
	// NextPage is the provider's next-page URL; blank means none was returned.
	NextPage string `json:"nextPage,omitempty"`
}

// ListDomainsInput selects one account domains page.
type ListDomainsInput struct {
	// Limit is at most 1000 domains; zero uses Mailgun's default.
	Limit int `json:"limit,omitempty"`
	// Skip is the zero-based offset into the account domain list.
	Skip int `json:"skip,omitempty"`
}

// Domain identifies one Mailgun sending domain.
type Domain struct {
	// Name is the DNS domain name.
	Name string `json:"name"`
	// State is the provider's verification state.
	State string `json:"state"`
}

// DomainsOutput holds one domains page and the account count.
type DomainsOutput struct {
	// Domains is the page of sending domains.
	Domains []Domain `json:"domains"`
	// TotalCount is the provider-reported account domain count.
	TotalCount int `json:"totalCount"`
}

// GetEventsOperation reads a sending domain's events.
type GetEventsOperation struct{ client *Client }

// ListDomainsOperation reads sending domains.
type ListDomainsOperation struct{ client *Client }

// GetEvents binds the event query to this client.
func (client *Client) GetEvents() GetEventsOperation { return GetEventsOperation{client} }

// ListDomains binds the domain query to this client.
func (client *Client) ListDomains() ListDomainsOperation { return ListDomainsOperation{client} }

// Definition returns the generated event contract.
func (GetEventsOperation) Definition() sdkgo.QueryDefinition { return GetEventsDefinition }

// Definition returns the generated domain contract.
func (ListDomainsOperation) Definition() sdkgo.QueryDefinition { return ListDomainsDefinition }

// Invoke reads one domain events page.
func (operation GetEventsOperation) Invoke(call sdkgo.Call, input GetEventsInput) sdkgo.QueryAttempt[EventsOutput] {
	branches := queryBranches{found: GetEventsBranchListed, rejected: GetEventsBranchProviderRejected, invalid: GetEventsBranchInvalidResponse, defect: GetEventsBranchDefect}
	if !domainPattern.MatchString(input.Domain) || input.Limit < 0 || input.Limit > 300 {
		return queryBranch("getEvents", branches.defect, EventsOutput{}, sdkgo.FailureValidation, "domain or limit is invalid")
	}
	query := url.Values{}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	return invokeJSONQuery(call, operation.client, "getEvents", branches, http.MethodGet, "/v3/"+input.Domain+"/events", query, nil, decodeEvents)
}

// Invoke lists one page of account sending domains.
func (operation ListDomainsOperation) Invoke(call sdkgo.Call, input ListDomainsInput) sdkgo.QueryAttempt[DomainsOutput] {
	branches := queryBranches{found: ListDomainsBranchListed, rejected: ListDomainsBranchProviderRejected, invalid: ListDomainsBranchInvalidResponse, defect: ListDomainsBranchDefect}
	if input.Limit < 0 || input.Limit > 1000 || input.Skip < 0 {
		return queryBranch("listDomains", branches.defect, DomainsOutput{}, sdkgo.FailureValidation, "limit or skip is invalid")
	}
	query := url.Values{}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Skip > 0 {
		query.Set("skip", strconv.Itoa(input.Skip))
	}
	return invokeJSONQuery(call, operation.client, "listDomains", branches, http.MethodGet, "/v4/domains", query, nil, decodeDomains)
}

var domainPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

func decodeEvents(body []byte) (EventsOutput, error) {
	var wire struct {
		Items  []Event `json:"items"`
		Paging struct {
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EventsOutput{}, err
	}
	if wire.Items == nil {
		return EventsOutput{}, fmt.Errorf("events missing")
	}
	return EventsOutput{Events: wire.Items, NextPage: wire.Paging.Next}, nil
}

func decodeDomains(body []byte) (DomainsOutput, error) {
	var wire struct {
		Items      []Domain `json:"items"`
		TotalCount *int     `json:"total_count"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DomainsOutput{}, err
	}
	if wire.Items == nil || wire.TotalCount == nil {
		return DomainsOutput{}, fmt.Errorf("domains fields missing")
	}
	return DomainsOutput{Domains: wire.Items, TotalCount: *wire.TotalCount}, nil
}
