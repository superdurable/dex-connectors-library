// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Organization is the safe organization metadata returned to a Flow.
type Organization struct {
	// ID is the positive Zendesk organization ID.
	ID int64 `json:"id"`
	// Name is the organization's Zendesk name.
	Name string `json:"name"`
	// ExternalID is the optional account-unique application identifier.
	ExternalID string `json:"externalId,omitempty"`
	// DomainNames are domains associated with the organization.
	DomainNames []string `json:"domainNames,omitempty"`
	// Tags are the organization's routing tags.
	Tags []string `json:"tags,omitempty"`
	// GroupID is the default ticket group when assigned.
	GroupID int64 `json:"groupId,omitempty"`
	// SharedTickets reports whether members can see each other's tickets.
	SharedTickets bool `json:"sharedTickets"`
	// SharedComments reports whether members can see each other's comments.
	SharedComments bool `json:"sharedComments"`
}

// ListOrganizationsInput selects one cursor page of organizations.
type ListOrganizationsInput struct {
	// PageSize is 1 through 100; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is a previous NextCursor; blank begins at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// ListOrganizationsOutput contains one organization page.
type ListOrganizationsOutput struct {
	// Organizations are visible on this page.
	Organizations []Organization `json:"organizations"`
	// NextCursor is blank when no later page exists.
	NextCursor string `json:"nextCursor"`
}

// CountOrganizationsInput requires no parameters.
type CountOrganizationsInput struct{}

// CountOrganizationsOutput contains Zendesk's approximate count.
type CountOrganizationsOutput struct {
	// Count may lag when an account has more than 100000 organizations.
	Count int64 `json:"count"`
}

// GetOrganizationInput selects one organization.
type GetOrganizationInput struct {
	// OrganizationID is the positive Zendesk organization ID.
	OrganizationID int64 `json:"organizationId"`
}

// GetOrganizationOutput contains one organization.
type GetOrganizationOutput struct {
	// Organization is the requested organization.
	Organization Organization `json:"organization"`
}

// GetOrganizationRelatedInput selects an organization for related counts.
type GetOrganizationRelatedInput struct {
	// OrganizationID is the positive Zendesk organization ID.
	OrganizationID int64 `json:"organizationId"`
}

// GetOrganizationRelatedOutput contains related ticket and user counts.
type GetOrganizationRelatedOutput struct {
	// TicketsCount is the count of tickets for the organization.
	TicketsCount int64 `json:"ticketsCount"`
	// UsersCount is the count of users in the organization.
	UsersCount int64 `json:"usersCount"`
}

// ListUserOrganizationsInput selects an organization's cursor page for a user.
type ListUserOrganizationsInput struct {
	// UserID is the positive Zendesk user ID.
	UserID int64 `json:"userId"`
	// PageSize is 1 through 100; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is a prior NextCursor; blank begins at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// ListUserOrganizationsOutput contains one user-organization page.
type ListUserOrganizationsOutput struct {
	// Organizations are associated with the selected user.
	Organizations []Organization `json:"organizations"`
	// NextCursor is blank when no later page exists.
	NextCursor string `json:"nextCursor"`
}

// GetUserRelatedInput selects a user for related counts.
type GetUserRelatedInput struct {
	// UserID is the positive Zendesk user ID.
	UserID int64 `json:"userId"`
}

// GetUserRelatedOutput contains ticket and subscription counts for a user.
type GetUserRelatedOutput struct {
	// AssignedTickets counts tickets assigned to the user.
	AssignedTickets int64 `json:"assignedTickets"`
	// RequestedTickets counts tickets requested by the user.
	RequestedTickets int64 `json:"requestedTickets"`
	// CCDTickets counts tickets on which the user is a collaborator.
	CCDTickets int64 `json:"ccdTickets"`
	// OrganizationSubscriptions counts organizations the user subscribes to.
	OrganizationSubscriptions int64 `json:"organizationSubscriptions"`
}

type zendeskOrganizationRecord struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	ExternalID     string   `json:"external_id"`
	DomainNames    []string `json:"domain_names"`
	Tags           []string `json:"tags"`
	GroupID        int64    `json:"group_id"`
	SharedTickets  bool     `json:"shared_tickets"`
	SharedComments bool     `json:"shared_comments"`
}

// ListOrganizationsOperation lists organizations.
type ListOrganizationsOperation struct{ client *Client }

// CountOrganizationsOperation counts organizations.
type CountOrganizationsOperation struct{ client *Client }

// GetOrganizationOperation reads one organization.
type GetOrganizationOperation struct{ client *Client }

// GetOrganizationRelatedOperation reads organization-related counts.
type GetOrganizationRelatedOperation struct{ client *Client }

// ListUserOrganizationsOperation lists a user's organizations.
type ListUserOrganizationsOperation struct{ client *Client }

// GetUserRelatedOperation reads user-related counts.
type GetUserRelatedOperation struct{ client *Client }

// ListOrganizations binds the organization list Query to this client.
func (client *Client) ListOrganizations() ListOrganizationsOperation {
	return ListOrganizationsOperation{client}
}

// CountOrganizations binds the organization count Query to this client.
func (client *Client) CountOrganizations() CountOrganizationsOperation {
	return CountOrganizationsOperation{client}
}

// GetOrganization binds the organization read Query to this client.
func (client *Client) GetOrganization() GetOrganizationOperation {
	return GetOrganizationOperation{client}
}

// GetOrganizationRelated binds the organization-related Query to this client.
func (client *Client) GetOrganizationRelated() GetOrganizationRelatedOperation {
	return GetOrganizationRelatedOperation{client}
}

// ListUserOrganizations binds the user-organization list Query to this client.
func (client *Client) ListUserOrganizations() ListUserOrganizationsOperation {
	return ListUserOrganizationsOperation{client}
}

// GetUserRelated binds the user-related Query to this client.
func (client *Client) GetUserRelated() GetUserRelatedOperation {
	return GetUserRelatedOperation{client}
}

// Definition returns the generated organization list Query contract.
func (ListOrganizationsOperation) Definition() sdkgo.QueryDefinition {
	return ListOrganizationsDefinition
}

// Definition returns the generated organization count Query contract.
func (CountOrganizationsOperation) Definition() sdkgo.QueryDefinition {
	return CountOrganizationsDefinition
}

// Definition returns the generated organization read Query contract.
func (GetOrganizationOperation) Definition() sdkgo.QueryDefinition { return GetOrganizationDefinition }

// Definition returns the generated organization-related Query contract.
func (GetOrganizationRelatedOperation) Definition() sdkgo.QueryDefinition {
	return GetOrganizationRelatedDefinition
}

// Definition returns the generated user-organization list Query contract.
func (ListUserOrganizationsOperation) Definition() sdkgo.QueryDefinition {
	return ListUserOrganizationsDefinition
}

// Definition returns the generated user-related Query contract.
func (GetUserRelatedOperation) Definition() sdkgo.QueryDefinition { return GetUserRelatedDefinition }

// Invoke reads one organization cursor page.
func (operation ListOrganizationsOperation) Invoke(call sdkgo.Call, input ListOrganizationsInput) sdkgo.QueryAttempt[ListOrganizationsOutput] {
	const operationName = "listOrganizations"
	pageSize, query, err := organizationPageQuery(input.PageSize, input.Cursor)
	if err != nil {
		return sdkgo.NewQueryBranch(ListOrganizationsBranchDefect, ListOrganizationsOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "pageSize or cursor is invalid"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListOrganizationsBranchDefect, ListOrganizationsOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/organizations", query: query})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[ListOrganizationsOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(ListOrganizationsBranchInvalidResponse, ListOrganizationsOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(ListOrganizationsBranchDefect, ListOrganizationsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListOrganizationsBranchProviderRejected, ListOrganizationsOutput{}, &result.failure, receipt)
	}
	organizations, nextCursor, err := decodeOrganizationPage(result.response.body, pageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(ListOrganizationsBranchInvalidResponse, ListOrganizationsOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid organization page"), receipt)
	}
	return sdkgo.NewQueryBranch(ListOrganizationsBranchListed, ListOrganizationsOutput{Organizations: organizations, NextCursor: nextCursor}, nil, receipt)
}

// Invoke reads Zendesk's approximate organization count.
func (operation CountOrganizationsOperation) Invoke(call sdkgo.Call, _ CountOrganizationsInput) sdkgo.QueryAttempt[CountOrganizationsOutput] {
	const operationName = "countOrganizations"
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(CountOrganizationsBranchDefect, CountOrganizationsOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/organizations/count"})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[CountOrganizationsOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(CountOrganizationsBranchInvalidResponse, CountOrganizationsOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(CountOrganizationsBranchDefect, CountOrganizationsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(CountOrganizationsBranchProviderRejected, CountOrganizationsOutput{}, &result.failure, receipt)
	}
	var envelope struct {
		Count *struct {
			Value int64 `json:"value"`
		} `json:"count"`
	}
	if err := json.Unmarshal(result.response.body, &envelope); err != nil || envelope.Count == nil || envelope.Count.Value < 0 {
		return sdkgo.NewQueryBranch(CountOrganizationsBranchInvalidResponse, CountOrganizationsOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid organization count"), receipt)
	}
	return sdkgo.NewQueryBranch(CountOrganizationsBranchCounted, CountOrganizationsOutput{Count: envelope.Count.Value}, nil, receipt)
}

// Invoke reads one organization.
func (operation GetOrganizationOperation) Invoke(call sdkgo.Call, input GetOrganizationInput) sdkgo.QueryAttempt[GetOrganizationOutput] {
	const operationName = "getOrganization"
	if input.OrganizationID < 1 {
		return sdkgo.NewQueryBranch(GetOrganizationBranchDefect, GetOrganizationOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "organizationId must be positive"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetOrganizationBranchDefect, GetOrganizationOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: organizationPath(input.OrganizationID)})
	receipt := operation.client.receipt(call, result.response, input.OrganizationID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[GetOrganizationOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetOrganizationBranchNotFound, GetOrganizationOutput{}, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetOrganizationBranchInvalidResponse, GetOrganizationOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetOrganizationBranchDefect, GetOrganizationOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetOrganizationBranchProviderRejected, GetOrganizationOutput{}, &result.failure, receipt)
	}
	organization, err := decodeOrganizationEnvelope(result.response.body)
	if err != nil || organization.ID != input.OrganizationID {
		return sdkgo.NewQueryBranch(GetOrganizationBranchInvalidResponse, GetOrganizationOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid organization"), receipt)
	}
	return sdkgo.NewQueryBranch(GetOrganizationBranchFound, GetOrganizationOutput{Organization: organization}, nil, receipt)
}

// Invoke reads related ticket and user counts for one organization.
func (operation GetOrganizationRelatedOperation) Invoke(call sdkgo.Call, input GetOrganizationRelatedInput) sdkgo.QueryAttempt[GetOrganizationRelatedOutput] {
	const operationName = "getOrganizationRelated"
	if input.OrganizationID < 1 {
		return sdkgo.NewQueryBranch(GetOrganizationRelatedBranchDefect, GetOrganizationRelatedOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "organizationId must be positive"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetOrganizationRelatedBranchDefect, GetOrganizationRelatedOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: organizationPath(input.OrganizationID) + "/related"})
	receipt := operation.client.receipt(call, result.response, input.OrganizationID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[GetOrganizationRelatedOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetOrganizationRelatedBranchNotFound, GetOrganizationRelatedOutput{}, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetOrganizationRelatedBranchInvalidResponse, GetOrganizationRelatedOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetOrganizationRelatedBranchDefect, GetOrganizationRelatedOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetOrganizationRelatedBranchProviderRejected, GetOrganizationRelatedOutput{}, &result.failure, receipt)
	}
	var envelope struct {
		Related *struct {
			TicketsCount int64 `json:"tickets_count"`
			UsersCount   int64 `json:"users_count"`
		} `json:"organization_related"`
	}
	if err := json.Unmarshal(result.response.body, &envelope); err != nil || envelope.Related == nil || envelope.Related.TicketsCount < 0 || envelope.Related.UsersCount < 0 {
		return sdkgo.NewQueryBranch(GetOrganizationRelatedBranchInvalidResponse, GetOrganizationRelatedOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned invalid related counts"), receipt)
	}
	return sdkgo.NewQueryBranch(GetOrganizationRelatedBranchFound, GetOrganizationRelatedOutput{TicketsCount: envelope.Related.TicketsCount, UsersCount: envelope.Related.UsersCount}, nil, receipt)
}

// Invoke reads organizations associated with one user.
func (operation ListUserOrganizationsOperation) Invoke(call sdkgo.Call, input ListUserOrganizationsInput) sdkgo.QueryAttempt[ListUserOrganizationsOutput] {
	const operationName = "listUserOrganizations"
	pageSize, query, err := organizationPageQuery(input.PageSize, input.Cursor)
	if input.UserID < 1 || err != nil {
		return sdkgo.NewQueryBranch(ListUserOrganizationsBranchDefect, ListUserOrganizationsOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "userId, pageSize, or cursor is invalid"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListUserOrganizationsBranchDefect, ListUserOrganizationsOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: userPath(input.UserID) + "/organizations", query: query})
	receipt := operation.client.receipt(call, result.response, input.UserID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[ListUserOrganizationsOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(ListUserOrganizationsBranchNotFound, ListUserOrganizationsOutput{}, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(ListUserOrganizationsBranchInvalidResponse, ListUserOrganizationsOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(ListUserOrganizationsBranchDefect, ListUserOrganizationsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListUserOrganizationsBranchProviderRejected, ListUserOrganizationsOutput{}, &result.failure, receipt)
	}
	organizations, nextCursor, err := decodeOrganizationPage(result.response.body, pageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(ListUserOrganizationsBranchInvalidResponse, ListUserOrganizationsOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid user-organization page"), receipt)
	}
	return sdkgo.NewQueryBranch(ListUserOrganizationsBranchListed, ListUserOrganizationsOutput{Organizations: organizations, NextCursor: nextCursor}, nil, receipt)
}

// Invoke reads ticket and subscription counts for one user.
func (operation GetUserRelatedOperation) Invoke(call sdkgo.Call, input GetUserRelatedInput) sdkgo.QueryAttempt[GetUserRelatedOutput] {
	const operationName = "getUserRelated"
	if input.UserID < 1 {
		return sdkgo.NewQueryBranch(GetUserRelatedBranchDefect, GetUserRelatedOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "userId must be positive"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetUserRelatedBranchDefect, GetUserRelatedOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: userPath(input.UserID) + "/related"})
	receipt := operation.client.receipt(call, result.response, input.UserID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[GetUserRelatedOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetUserRelatedBranchNotFound, GetUserRelatedOutput{}, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetUserRelatedBranchInvalidResponse, GetUserRelatedOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetUserRelatedBranchDefect, GetUserRelatedOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetUserRelatedBranchProviderRejected, GetUserRelatedOutput{}, &result.failure, receipt)
	}
	var envelope struct {
		Related *struct {
			AssignedTickets           int64 `json:"assigned_tickets"`
			RequestedTickets          int64 `json:"requested_tickets"`
			CCDTickets                int64 `json:"ccd_tickets"`
			OrganizationSubscriptions int64 `json:"organization_subscriptions"`
		} `json:"user_related"`
	}
	if err := json.Unmarshal(result.response.body, &envelope); err != nil || envelope.Related == nil || envelope.Related.AssignedTickets < 0 || envelope.Related.RequestedTickets < 0 || envelope.Related.CCDTickets < 0 || envelope.Related.OrganizationSubscriptions < 0 {
		return sdkgo.NewQueryBranch(GetUserRelatedBranchInvalidResponse, GetUserRelatedOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned invalid user-related counts"), receipt)
	}
	return sdkgo.NewQueryBranch(GetUserRelatedBranchFound, GetUserRelatedOutput{AssignedTickets: envelope.Related.AssignedTickets, RequestedTickets: envelope.Related.RequestedTickets, CCDTickets: envelope.Related.CCDTickets, OrganizationSubscriptions: envelope.Related.OrganizationSubscriptions}, nil, receipt)
}

func organizationPageQuery(size int, cursor string) (int, url.Values, error) {
	if size < 0 || size > 100 || len(cursor) > maximumCursorLength || strings.ContainsAny(cursor, "\r\n\t") {
		return 0, nil, errors.New("invalid organization page")
	}
	if size == 0 {
		size = 25
	}
	query := url.Values{"page[size]": {strconv.Itoa(size)}}
	if cursor != "" {
		query.Set("page[after]", cursor)
	}
	return size, query, nil
}

func decodeOrganizationPage(body []byte, pageSize int) ([]Organization, string, error) {
	var envelope struct {
		Organizations []zendeskOrganizationRecord `json:"organizations"`
		Meta          struct {
			HasMore     bool   `json:"has_more"`
			AfterCursor string `json:"after_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, "", err
	}
	if envelope.Organizations == nil || len(envelope.Organizations) > pageSize || envelope.Meta.HasMore && envelope.Meta.AfterCursor == "" {
		return nil, "", errors.New("invalid organization page")
	}
	organizations := make([]Organization, 0, len(envelope.Organizations))
	for _, record := range envelope.Organizations {
		organization, err := decodeOrganizationRecord(record)
		if err != nil {
			return nil, "", err
		}
		organizations = append(organizations, organization)
	}
	if envelope.Meta.HasMore {
		return organizations, envelope.Meta.AfterCursor, nil
	}
	return organizations, "", nil
}

func decodeOrganizationEnvelope(body []byte) (Organization, error) {
	var envelope struct {
		Organization *zendeskOrganizationRecord `json:"organization"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Organization{}, err
	}
	if envelope.Organization == nil {
		return Organization{}, errors.New("missing organization")
	}
	return decodeOrganizationRecord(*envelope.Organization)
}

func decodeOrganizationRecord(record zendeskOrganizationRecord) (Organization, error) {
	if record.ID < 1 || strings.TrimSpace(record.Name) == "" {
		return Organization{}, errors.New("organization has no ID or name")
	}
	return Organization{ID: record.ID, Name: record.Name, ExternalID: record.ExternalID, DomainNames: record.DomainNames, Tags: record.Tags, GroupID: record.GroupID, SharedTickets: record.SharedTickets, SharedComments: record.SharedComments}, nil
}

func organizationPath(organizationID int64) string {
	return "/organizations/" + strconv.FormatInt(organizationID, 10)
}
