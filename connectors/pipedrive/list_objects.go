// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListSortField is a Pipedrive list sort field, passed through as sort_by.
type ListSortField string

const (
	// ListSortFieldID orders by record ID, Pipedrive's default.
	ListSortFieldID ListSortField = "id"
	// ListSortFieldUpdateTime orders by last change, for changed-since polling.
	ListSortFieldUpdateTime ListSortField = "update_time"
	// ListSortFieldAddTime orders by creation time.
	ListSortFieldAddTime ListSortField = "add_time"
)

// SortDirection is a Pipedrive list sort direction, passed through as sort_direction.
type SortDirection string

const (
	// SortDirectionAscending returns the smallest or oldest values first, Pipedrive's default.
	SortDirectionAscending SortDirection = "asc"
	// SortDirectionDescending returns the largest or newest values first.
	SortDirectionDescending SortDirection = "desc"
)

// ListObjectsInput selects one bounded page of full records. Every filter is
// optional; filters combine with AND.
type ListObjectsInput struct {
	// ObjectType is the object type to list.
	ObjectType ObjectType `json:"objectType"`
	// UpdatedSince keeps records whose update_time is at or after this time; zero sets no lower bound.
	UpdatedSince time.Time `json:"updatedSince,omitzero"`
	// UpdatedUntil keeps records whose update_time is before this time; zero sets no upper bound.
	UpdatedUntil time.Time `json:"updatedUntil,omitzero"`
	// OwnerID keeps records owned by one Pipedrive user.
	OwnerID string `json:"ownerId,omitempty"`
	// OrganizationID keeps persons or deals linked to one organization.
	OrganizationID string `json:"organizationId,omitempty"`
	// PersonID keeps deals linked to one person.
	PersonID string `json:"personId,omitempty"`
	// PipelineID keeps deals in one pipeline.
	PipelineID string `json:"pipelineId,omitempty"`
	// StageID keeps deals in one stage.
	StageID string `json:"stageId,omitempty"`
	// Statuses keeps deals with one of these statuses; empty lists every deal that is not deleted.
	Statuses []DealStatus `json:"statuses,omitempty"`
	// CustomFieldKeys limits returned custom fields to at most 15 keys; empty returns all of them.
	CustomFieldKeys []string `json:"customFieldKeys,omitempty"`
	// SortBy orders the page; empty uses id, or update_time when UpdatedSince is set.
	SortBy ListSortField `json:"sortBy,omitempty"`
	// SortDirection orders ascending or descending; empty is ascending.
	SortDirection SortDirection `json:"sortDirection,omitempty"`
	// Limit is the page size from 1 through 500; zero uses 25.
	Limit int `json:"limit,omitempty"`
	// Cursor is the NextCursor of the previous page; empty starts at the first record.
	Cursor string `json:"cursor,omitempty"`
}

// ListObjectsOperation implements the bounded list Query.
type ListObjectsOperation struct{ client *Client }

var listBranches = operationBranches{
	rejected: ListObjectsBranchProviderRejected, invalidResponse: ListObjectsBranchInvalidResponse, defect: ListObjectsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ListObjectsOperation) Definition() sdkgo.QueryDefinition { return ListObjectsDefinition }

// Invoke sends one list request and returns one bounded page of full records.
func (operation ListObjectsOperation) Invoke(call sdkgo.Call, input ListObjectsInput) sdkgo.QueryAttempt[ObjectPage] {
	const operationID = "listObjects"
	query, err := input.listQuery()
	if err != nil {
		return sdkgo.NewQueryBranch(ListObjectsBranchDefect, ObjectPage{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, operationID)
	if failure != nil {
		return queryAttemptForSession[ObjectPage](failure, listBranches)
	}
	defer cancel()
	result := operation.client.exchange(session, providerRequest{method: http.MethodGet, path: collectionPath(input.ObjectType), query: query})
	receipt := operation.client.receipt(call, result.response, "")
	if result.outcome != exchangeSucceeded {
		return queryAttemptForExchange[ObjectPage](result, receipt, listBranches)
	}
	page, err := decodeListPage(input.ObjectType, result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListObjectsBranchInvalidResponse, ObjectPage{}, providerFailurePointer(operationID, sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListObjectsBranchListed, page, nil, receipt)
}

func (input ListObjectsInput) listQuery() (url.Values, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	query := url.Values{}
	if !input.UpdatedSince.IsZero() {
		query.Set("updated_since", input.UpdatedSince.UTC().Format(time.RFC3339))
	}
	if !input.UpdatedUntil.IsZero() {
		if !input.UpdatedSince.IsZero() && !input.UpdatedUntil.After(input.UpdatedSince) {
			return nil, errors.New("updatedUntil must be after updatedSince")
		}
		query.Set("updated_until", input.UpdatedUntil.UTC().Format(time.RFC3339))
	}
	if err := input.addRecordFilters(query); err != nil {
		return nil, err
	}
	if err := input.addSort(query); err != nil {
		return nil, err
	}
	if len(input.CustomFieldKeys) > MaximumListedCustomFields {
		return nil, fmt.Errorf("at most %d custom field keys can be requested", MaximumListedCustomFields)
	}
	for _, key := range input.CustomFieldKeys {
		if !customFieldKeyPattern.MatchString(key) {
			return nil, errors.New("a custom field key must be Pipedrive's 40-character lowercase hexadecimal key")
		}
	}
	if len(input.CustomFieldKeys) > 0 {
		query.Set("custom_fields", strings.Join(input.CustomFieldKeys, ","))
	}
	limit, err := validatePageLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	query.Set("limit", strconv.Itoa(limit))
	if err := validateCursor(input.Cursor); err != nil {
		return nil, err
	}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	return query, nil
}

// addRecordFilters checks that each filter applies to the object type, as Pipedrive's list endpoints define.
func (input ListObjectsInput) addRecordFilters(query url.Values) error {
	isDeals := input.ObjectType == ObjectTypeDeals
	filters := []struct {
		inputName, queryName, value string
		isSupported                 bool
	}{
		{"ownerId", "owner_id", input.OwnerID, true},
		{"organizationId", "org_id", input.OrganizationID, input.ObjectType != ObjectTypeOrganizations},
		{"personId", "person_id", input.PersonID, isDeals},
		{"pipelineId", "pipeline_id", input.PipelineID, isDeals},
		{"stageId", "stage_id", input.StageID, isDeals},
	}
	for _, filter := range filters {
		if filter.value == "" {
			continue
		}
		if !filter.isSupported {
			return fmt.Errorf("%s does not filter %s", filter.inputName, input.ObjectType)
		}
		if err := validateRecordID(filter.inputName, filter.value); err != nil {
			return err
		}
		query.Set(filter.queryName, filter.value)
	}
	if len(input.Statuses) > 0 && !isDeals {
		return errors.New("statuses filter only deals")
	}
	statuses, err := joinDealStatuses(input.Statuses, true)
	if err != nil {
		return err
	}
	if statuses != "" {
		query.Set("status", statuses)
	}
	return nil
}

func (input ListObjectsInput) addSort(query url.Values) error {
	sortBy := input.SortBy
	if sortBy == "" && !input.UpdatedSince.IsZero() {
		sortBy = ListSortFieldUpdateTime
	}
	if sortBy != "" {
		if !slices.Contains([]ListSortField{ListSortFieldID, ListSortFieldUpdateTime, ListSortFieldAddTime}, sortBy) {
			return errors.New("sortBy must be id, update_time, or add_time")
		}
		query.Set("sort_by", string(sortBy))
	}
	if input.SortDirection != "" {
		if input.SortDirection != SortDirectionAscending && input.SortDirection != SortDirectionDescending {
			return errors.New("sortDirection must be asc or desc")
		}
		query.Set("sort_direction", string(input.SortDirection))
	}
	return nil
}
