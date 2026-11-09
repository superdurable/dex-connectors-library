// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListOrganizationsInput selects a one-based page; zero defaults to the first page.
type ListOrganizationsInput struct {
	Page int `json:"page,omitempty"`
}

// OrganizationMetadata identifies an organization without member details.
type OrganizationMetadata struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OrganizationsPage is one page of organizations visible to the connected account.
type OrganizationsPage struct {
	Organizations []OrganizationMetadata `json:"organizations"`
	PageNumber    int                    `json:"pageNumber"`
	PageCount     int                    `json:"pageCount"`
}

// ListEventsInput selects one organization's one-based event page.
type ListEventsInput struct {
	OrganizationID string `json:"organizationId"`
	Page           int    `json:"page,omitempty"`
}

// EventMetadata contains an event's identity, name, and status.
type EventMetadata struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// EventsPage is one page of an organization's events.
type EventsPage struct {
	Events     []EventMetadata `json:"events"`
	PageNumber int             `json:"pageNumber"`
	PageCount  int             `json:"pageCount"`
}

// GetAttendeeInput selects an attendee within an event.
type GetAttendeeInput struct {
	EventID    string `json:"eventId"`
	AttendeeID string `json:"attendeeId"`
}

// AttendeeMetadata excludes the attendee profile, answers, barcode, and email.
type AttendeeMetadata struct {
	ID        string `json:"id"`
	EventID   string `json:"eventId"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// ListOrganizationsOperation retrieves accessible organization identities.
type ListOrganizationsOperation struct{ client *Client }

// ListEventsOperation retrieves an organization's event page.
type ListEventsOperation struct{ client *Client }

// GetAttendeeOperation retrieves one attendee's metadata.
type GetAttendeeOperation struct{ client *Client }

// ListOrganizations binds the query to this client.
func (client *Client) ListOrganizations() ListOrganizationsOperation {
	return ListOrganizationsOperation{client}
}

// ListEvents binds the query to this client.
func (client *Client) ListEvents() ListEventsOperation { return ListEventsOperation{client} }

// GetAttendee binds the query to this client.
func (client *Client) GetAttendee() GetAttendeeOperation { return GetAttendeeOperation{client} }

// Definition returns the manifest-generated operation contract.
func (ListOrganizationsOperation) Definition() sdkgo.QueryDefinition {
	return ListOrganizationsDefinition
}

// Definition returns the manifest-generated operation contract.
func (ListEventsOperation) Definition() sdkgo.QueryDefinition { return ListEventsDefinition }

// Definition returns the manifest-generated operation contract.
func (GetAttendeeOperation) Definition() sdkgo.QueryDefinition { return GetAttendeeDefinition }

var numericID = regexp.MustCompile(`^[0-9]{1,24}$`)

// Invoke reads one organization page.
func (operation ListOrganizationsOperation) Invoke(call sdkgo.Call, input ListOrganizationsInput) sdkgo.QueryAttempt[OrganizationsPage] {
	branches := queryBranches{found: ListOrganizationsBranchListed, rejected: ListOrganizationsBranchProviderRejected, invalid: ListOrganizationsBranchInvalidResponse, defect: ListOrganizationsBranchDefect}
	page, ok := pageNumber(input.Page)
	if !ok {
		return queryBranch("listOrganizations", branches.defect, OrganizationsPage{}, sdkgo.FailureValidation, "page must be between 1 and 10000")
	}
	return invokeQuery(call, operation.client, "listOrganizations", "/users/me/organizations/?page="+strconv.Itoa(page), branches, decodeOrganizations)
}

// Invoke reads one event page after validating the organization ID.
func (operation ListEventsOperation) Invoke(call sdkgo.Call, input ListEventsInput) sdkgo.QueryAttempt[EventsPage] {
	branches := queryBranches{found: ListEventsBranchListed, notFound: ListEventsBranchNotFound, rejected: ListEventsBranchProviderRejected, invalid: ListEventsBranchInvalidResponse, defect: ListEventsBranchDefect}
	page, ok := pageNumber(input.Page)
	if !numericID.MatchString(input.OrganizationID) || !ok {
		return queryBranch("listEvents", branches.defect, EventsPage{}, sdkgo.FailureValidation, "organization ID or page is invalid")
	}
	path := "/organizations/" + input.OrganizationID + "/events/?page=" + strconv.Itoa(page)
	return invokeQuery(call, operation.client, "listEvents", path, branches, decodeEvents)
}

// Invoke reads attendee metadata without profile or answers.
func (operation GetAttendeeOperation) Invoke(call sdkgo.Call, input GetAttendeeInput) sdkgo.QueryAttempt[AttendeeMetadata] {
	branches := queryBranches{found: GetAttendeeBranchFound, notFound: GetAttendeeBranchNotFound, rejected: GetAttendeeBranchProviderRejected, invalid: GetAttendeeBranchInvalidResponse, defect: GetAttendeeBranchDefect}
	if !numericID.MatchString(input.EventID) || !numericID.MatchString(input.AttendeeID) {
		return queryBranch("getAttendee", branches.defect, AttendeeMetadata{}, sdkgo.FailureValidation, "event ID or attendee ID is invalid")
	}
	path := "/events/" + input.EventID + "/attendees/" + input.AttendeeID + "/"
	return invokeQuery(call, operation.client, "getAttendee", path, branches, decodeAttendee)
}

func pageNumber(page int) (int, bool) {
	if page == 0 {
		return 1, true
	}
	return page, page > 0 && page <= 10000
}

func decodeOrganizations(body []byte) (OrganizationsPage, error) {
	var wire struct {
		Organizations []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"organizations"`
		Pagination pagination `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OrganizationsPage{}, err
	}
	if wire.Organizations == nil || !wire.Pagination.valid() {
		return OrganizationsPage{}, fmt.Errorf("organization page missing")
	}
	page := OrganizationsPage{Organizations: make([]OrganizationMetadata, 0, len(wire.Organizations)), PageNumber: wire.Pagination.PageNumber, PageCount: wire.Pagination.PageCount}
	for _, organization := range wire.Organizations {
		if !numericID.MatchString(organization.ID) {
			return OrganizationsPage{}, fmt.Errorf("organization ID missing")
		}
		page.Organizations = append(page.Organizations, OrganizationMetadata{ID: organization.ID, Name: organization.Name})
	}
	return page, nil
}

func decodeEvents(body []byte) (EventsPage, error) {
	var wire struct {
		Events []struct {
			ID   string `json:"id"`
			Name struct {
				Text string `json:"text"`
			} `json:"name"`
			Status string `json:"status"`
		} `json:"events"`
		Pagination pagination `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EventsPage{}, err
	}
	if wire.Events == nil || !wire.Pagination.valid() {
		return EventsPage{}, fmt.Errorf("event page missing")
	}
	page := EventsPage{Events: make([]EventMetadata, 0, len(wire.Events)), PageNumber: wire.Pagination.PageNumber, PageCount: wire.Pagination.PageCount}
	for _, event := range wire.Events {
		if !numericID.MatchString(event.ID) {
			return EventsPage{}, fmt.Errorf("event ID missing")
		}
		page.Events = append(page.Events, EventMetadata{ID: event.ID, Name: event.Name.Text, Status: event.Status})
	}
	return page, nil
}

func decodeAttendee(body []byte) (AttendeeMetadata, error) {
	var wire struct {
		ID        string `json:"id"`
		EventID   string `json:"event_id"`
		Status    string `json:"status"`
		CreatedAt string `json:"created"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AttendeeMetadata{}, err
	}
	if !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.EventID) {
		return AttendeeMetadata{}, fmt.Errorf("attendee identity missing")
	}
	return AttendeeMetadata{ID: wire.ID, EventID: wire.EventID, Status: wire.Status, CreatedAt: wire.CreatedAt}, nil
}

type pagination struct {
	PageNumber int `json:"page_number"`
	PageCount  int `json:"page_count"`
}

func (page pagination) valid() bool { return page.PageNumber > 0 && page.PageCount >= 0 }
