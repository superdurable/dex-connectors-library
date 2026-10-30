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
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// SearchField is one Pipedrive search field, passed through as the fields query value.
type SearchField string

const (
	// SearchFieldName searches person and organization names.
	SearchFieldName SearchField = "name"
	// SearchFieldTitle searches deal titles.
	SearchFieldTitle SearchField = "title"
	// SearchFieldEmail searches person email addresses.
	SearchFieldEmail SearchField = "email"
	// SearchFieldPhone searches person phone numbers.
	SearchFieldPhone SearchField = "phone"
	// SearchFieldAddress searches organization addresses.
	SearchFieldAddress SearchField = "address"
	// SearchFieldNotes searches notes attached to the record.
	SearchFieldNotes SearchField = "notes"
	// SearchFieldCustomFields searches address, text, number, monetary, and phone custom fields.
	SearchFieldCustomFields SearchField = "custom_fields"
)

// MaximumSearchTermCharacters bounds a search term; Pipedrive needs at least 2, or 1 with ExactMatch.
const MaximumSearchTermCharacters = 255

// searchFieldsByObjectType are the fields each Pipedrive API v2 search endpoint accepts.
var searchFieldsByObjectType = map[ObjectType][]SearchField{
	ObjectTypePersons:       {SearchFieldName, SearchFieldEmail, SearchFieldPhone, SearchFieldNotes, SearchFieldCustomFields},
	ObjectTypeOrganizations: {SearchFieldName, SearchFieldAddress, SearchFieldNotes, SearchFieldCustomFields},
	ObjectTypeDeals:         {SearchFieldTitle, SearchFieldNotes, SearchFieldCustomFields},
}

// SearchObjectsInput selects one bounded page of partial search results.
type SearchObjectsInput struct {
	// ObjectType is the object type to search.
	ObjectType ObjectType `json:"objectType"`
	// Term is the search text, at least 2 characters, or 1 with ExactMatch.
	Term string `json:"term"`
	// Fields limits where Term is searched; empty searches every field the object type supports.
	Fields []SearchField `json:"fields,omitempty"`
	// ExactMatch returns only records whose field equals Term, ignoring case.
	ExactMatch bool `json:"exactMatch,omitempty"`
	// OrganizationID limits a persons or deals search to one organization.
	OrganizationID string `json:"organizationId,omitempty"`
	// PersonID limits a deals search to one person.
	PersonID string `json:"personId,omitempty"`
	// Statuses limits a deals search to open, won, or lost deals.
	Statuses []DealStatus `json:"statuses,omitempty"`
	// Limit is the page size from 1 through 500; zero uses 25.
	Limit int `json:"limit,omitempty"`
	// Cursor is the NextCursor of the previous page; empty starts at the first result.
	Cursor string `json:"cursor,omitempty"`
}

// SearchObjectsOperation implements the search Query.
type SearchObjectsOperation struct{ client *Client }

var searchBranches = operationBranches{
	rejected: SearchObjectsBranchProviderRejected, invalidResponse: SearchObjectsBranchInvalidResponse, defect: SearchObjectsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (SearchObjectsOperation) Definition() sdkgo.QueryDefinition { return SearchObjectsDefinition }

// Invoke sends one search request and returns one bounded page of partial records.
func (operation SearchObjectsOperation) Invoke(call sdkgo.Call, input SearchObjectsInput) sdkgo.QueryAttempt[ObjectPage] {
	const operationID = "searchObjects"
	query, err := input.searchQuery()
	if err != nil {
		return sdkgo.NewQueryBranch(SearchObjectsBranchDefect, ObjectPage{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, operationID)
	if failure != nil {
		return queryAttemptForSession[ObjectPage](failure, searchBranches)
	}
	defer cancel()
	result := operation.client.exchange(session, providerRequest{method: http.MethodGet, path: searchPath(input.ObjectType), query: query})
	receipt := operation.client.receipt(call, result.response, "")
	if result.outcome != exchangeSucceeded {
		return queryAttemptForExchange[ObjectPage](result, receipt, searchBranches)
	}
	page, err := decodeSearchPage(input.ObjectType, result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchObjectsBranchInvalidResponse, ObjectPage{}, providerFailurePointer(operationID, sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchObjectsBranchSearched, page, nil, receipt)
}

func (input SearchObjectsInput) searchQuery() (url.Values, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	if err := validateSearchTerm(input.Term, input.ExactMatch); err != nil {
		return nil, err
	}
	query := url.Values{"term": {input.Term}}
	if len(input.Fields) > 0 {
		names := make([]string, 0, len(input.Fields))
		for _, field := range input.Fields {
			if !slices.Contains(searchFieldsByObjectType[input.ObjectType], field) {
				return nil, fmt.Errorf("search field %q is not searchable for %s", field, input.ObjectType)
			}
			if !slices.Contains(names, string(field)) {
				names = append(names, string(field))
			}
		}
		query.Set("fields", strings.Join(names, ","))
	}
	if input.ExactMatch {
		query.Set("exact_match", "true")
	}
	if input.OrganizationID != "" {
		if input.ObjectType == ObjectTypeOrganizations {
			return nil, errors.New("organizationId filters only persons and deals searches")
		}
		if err := validateRecordID("organizationId", input.OrganizationID); err != nil {
			return nil, err
		}
		query.Set("organization_id", input.OrganizationID)
	}
	if err := input.addDealFilters(query); err != nil {
		return nil, err
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

func (input SearchObjectsInput) addDealFilters(query url.Values) error {
	if input.ObjectType != ObjectTypeDeals {
		if input.PersonID != "" || len(input.Statuses) > 0 {
			return errors.New("personId and statuses filter only deals searches")
		}
		return nil
	}
	if input.PersonID != "" {
		if err := validateRecordID("personId", input.PersonID); err != nil {
			return err
		}
		query.Set("person_id", input.PersonID)
	}
	statuses, err := joinDealStatuses(input.Statuses, false)
	if err != nil {
		return err
	}
	if statuses != "" {
		query.Set("status", statuses)
	}
	return nil
}

func validateSearchTerm(term string, isExactMatch bool) error {
	length := utf8.RuneCountInString(strings.TrimSpace(term))
	minimum := 2
	if isExactMatch {
		minimum = 1
	}
	if length < minimum || utf8.RuneCountInString(term) > MaximumSearchTermCharacters {
		return fmt.Errorf("search term needs %d to %d characters", minimum, MaximumSearchTermCharacters)
	}
	return nil
}

// joinDealStatuses validates deal statuses; deleted is accepted only where the list endpoint allows it.
func joinDealStatuses(statuses []DealStatus, allowsDeleted bool) (string, error) {
	names := make([]string, 0, len(statuses))
	for _, status := range statuses {
		switch {
		case status == DealStatusOpen || status == DealStatusWon || status == DealStatusLost:
		case status == DealStatusDeleted && allowsDeleted:
		default:
			return "", fmt.Errorf("deal status %q is not supported here", status)
		}
		if !slices.Contains(names, string(status)) {
			names = append(names, string(status))
		}
	}
	return strings.Join(names, ","), nil
}

func searchPath(objectType ObjectType) string {
	return "/api/v2/" + string(objectType) + "/search"
}
