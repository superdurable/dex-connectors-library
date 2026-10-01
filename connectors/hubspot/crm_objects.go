// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ObjectType names one HubSpot CRM object type. Its value is HubSpot's
// object name in API paths.
type ObjectType string

const (
	// ObjectTypeContacts selects HubSpot contacts, object type ID 0-1.
	ObjectTypeContacts ObjectType = "contacts"
	// ObjectTypeCompanies selects HubSpot companies, object type ID 0-2.
	ObjectTypeCompanies ObjectType = "companies"
	// ObjectTypeDeals selects HubSpot deals, object type ID 0-3.
	ObjectTypeDeals ObjectType = "deals"
)

// FilterOperator is one HubSpot CRM search filter operator.
type FilterOperator string

const (
	// FilterOperatorEqual matches a property equal to Value.
	FilterOperatorEqual FilterOperator = "EQ"
	// FilterOperatorNotEqual matches a property not equal to Value.
	FilterOperatorNotEqual FilterOperator = "NEQ"
	// FilterOperatorLessThan matches a property less than Value.
	FilterOperatorLessThan FilterOperator = "LT"
	// FilterOperatorLessThanOrEqual matches a property less than or equal to Value.
	FilterOperatorLessThanOrEqual FilterOperator = "LTE"
	// FilterOperatorGreaterThan matches a property greater than Value.
	FilterOperatorGreaterThan FilterOperator = "GT"
	// FilterOperatorGreaterThanOrEqual matches a property greater than or equal to Value.
	FilterOperatorGreaterThanOrEqual FilterOperator = "GTE"
	// FilterOperatorBetween matches a property from Value through HighValue.
	FilterOperatorBetween FilterOperator = "BETWEEN"
	// FilterOperatorIn matches a property exactly equal to one of Values.
	FilterOperatorIn FilterOperator = "IN"
	// FilterOperatorNotIn matches a property equal to none of Values.
	FilterOperatorNotIn FilterOperator = "NOT_IN"
	// FilterOperatorHasProperty matches a record with any value for the property.
	FilterOperatorHasProperty FilterOperator = "HAS_PROPERTY"
	// FilterOperatorNotHasProperty matches a record without a value for the property.
	FilterOperatorNotHasProperty FilterOperator = "NOT_HAS_PROPERTY"
	// FilterOperatorContainsToken matches a property containing the Value token; * is a wildcard.
	FilterOperatorContainsToken FilterOperator = "CONTAINS_TOKEN"
	// FilterOperatorNotContainsToken matches a property not containing the Value token.
	FilterOperatorNotContainsToken FilterOperator = "NOT_CONTAINS_TOKEN"
)

// SortDirection orders CRM search results by one property.
type SortDirection string

const (
	// SortDirectionAscending returns the smallest or oldest values first.
	SortDirectionAscending SortDirection = "ASCENDING"
	// SortDirectionDescending returns the largest or newest values first.
	SortDirectionDescending SortDirection = "DESCENDING"
)

// Search and write bounds. HubSpot documents the filter, page, result, and
// body limits; the property counts bound Flow payload size.
const (
	// MaximumFilterGroups is HubSpot's limit on OR-ed filter groups in one search.
	MaximumFilterGroups = 5
	// MaximumFiltersPerGroup is HubSpot's limit on AND-ed filters in one group.
	MaximumFiltersPerGroup = 6
	// MaximumFilters is HubSpot's limit on filters across all groups of one search.
	MaximumFilters = 18
	// MaximumFilterValues bounds the Values of one IN or NOT_IN filter.
	MaximumFilterValues = 100
	// DefaultSearchLimit is the page size used when SearchObjectsInput.Limit is zero, matching HubSpot's default.
	DefaultSearchLimit = 10
	// MaximumSearchLimit is HubSpot's largest search page.
	MaximumSearchLimit = 200
	// MaximumSearchResults is HubSpot's limit on results reachable through paging for one search.
	MaximumSearchResults = 10000
	// MaximumSearchRequestBytes is HubSpot's limit on the encoded search request body.
	MaximumSearchRequestBytes = 3000
	// MaximumProperties bounds requested property names and written property values per call.
	MaximumProperties = 100
	// MaximumPropertyValueCharacters is HubSpot's limit on one string property value.
	MaximumPropertyValueCharacters = 65536
)

var (
	propertyNamePattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_]{0,99}$`)
	associationPropertyPattern   = regexp.MustCompile(`^associations\.[a-z][a-z0-9_]{0,63}$`)
	recordIDPattern              = regexp.MustCompile(`^[0-9]{1,20}$`)
	searchCursorPattern          = regexp.MustCompile(`^[0-9]{1,5}$`)
	errNoRecordInUpsertResponse  = errors.New("HubSpot upsert response has no record")
	errManyRecordsInUpsertResult = errors.New("HubSpot upsert response has more than one record")
)

// CRMObject is one HubSpot CRM record.
type CRMObject struct {
	// ObjectType is the record's object type.
	ObjectType ObjectType `json:"objectType"`
	// ID is HubSpot's record ID (hs_object_id), a decimal string.
	ID string `json:"id"`
	// Properties holds the returned property values keyed by internal property
	// name. HubSpot returns every value as a string; a property without a value
	// is absent.
	Properties map[string]string `json:"properties,omitempty"`
	// CreatedAt is when HubSpot created the record, when reported.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// UpdatedAt is when HubSpot last modified the record, when reported.
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	// Archived reports whether the record is archived.
	Archived bool `json:"archived"`
	// URL is the HubSpot app link to the record when HubSpot returns one.
	URL string `json:"url,omitempty"`
}

// SearchFilter is one typed CRM search condition.
type SearchFilter struct {
	// PropertyName is an internal property name such as email, or an
	// association pseudo-property such as associations.contact.
	PropertyName string `json:"propertyName"`
	// Operator selects the comparison.
	Operator FilterOperator `json:"operator"`
	// Value is the comparison value for EQ, NEQ, LT, LTE, GT, GTE, BETWEEN,
	// CONTAINS_TOKEN, and NOT_CONTAINS_TOKEN. Dates are Unix milliseconds.
	Value string `json:"value,omitempty"`
	// HighValue is the inclusive upper bound for BETWEEN.
	HighValue string `json:"highValue,omitempty"`
	// Values lists the candidates for IN and NOT_IN. HubSpot requires lowercase
	// values when the property is a string property.
	Values []string `json:"values,omitempty"`
}

// SearchFilterGroup AND-s its filters. A search OR-s its groups.
type SearchFilterGroup struct {
	// Filters are the conditions that must all match.
	Filters []SearchFilter `json:"filters"`
}

// SearchSort orders search results by one property.
type SearchSort struct {
	// PropertyName is the internal property name to sort by.
	PropertyName string `json:"propertyName"`
	// Direction is ascending or descending.
	Direction SortDirection `json:"direction"`
}

// SearchObjectsInput selects one bounded page of matching records.
type SearchObjectsInput struct {
	// ObjectType is the object type to search.
	ObjectType ObjectType `json:"objectType"`
	// FilterGroups are OR-ed; empty matches every record. HubSpot allows five
	// groups, six filters per group, and eighteen filters in total.
	FilterGroups []SearchFilterGroup `json:"filterGroups,omitempty"`
	// Query optionally searches the object's default text properties.
	Query string `json:"query,omitempty"`
	// Sort optionally orders results; without it HubSpot returns oldest records first.
	Sort *SearchSort `json:"sort,omitempty"`
	// Properties lists the property values to return; empty returns HubSpot's
	// default properties for the object type.
	Properties []string `json:"properties,omitempty"`
	// Limit is the page size from 1 through 200; zero uses 10.
	Limit int `json:"limit,omitempty"`
	// After is the NextAfter cursor of the previous page; empty starts at the
	// first result. Paging cannot pass HubSpot's 10,000th result.
	After string `json:"after,omitempty"`
}

// ObjectPage is one page of CRM search results.
type ObjectPage struct {
	// ObjectType is the searched object type.
	ObjectType ObjectType `json:"objectType"`
	// Total is HubSpot's count of all matching records.
	Total int `json:"total"`
	// Objects are the records on this page, possibly none.
	Objects []CRMObject `json:"objects"`
	// NextAfter is the cursor for the next page, empty on the last page.
	NextAfter string `json:"nextAfter,omitempty"`
}

// GetObjectInput identifies one record.
type GetObjectInput struct {
	// ObjectType is the record's object type.
	ObjectType ObjectType `json:"objectType"`
	// ObjectID is HubSpot's decimal record ID.
	ObjectID string `json:"objectId"`
	// Properties lists the property values to return; empty returns HubSpot's
	// default properties for the object type.
	Properties []string `json:"properties,omitempty"`
}

// UpsertObjectInput creates or updates one record identified by a unique property value.
type UpsertObjectInput struct {
	// ObjectType is the record's object type.
	ObjectType ObjectType `json:"objectType"`
	// IDProperty is a unique identifier property: email for contacts, or a
	// custom property created with unique values for any object type.
	IDProperty string `json:"idProperty"`
	// IDValue is the unique value that identifies the record, such as the email address.
	IDValue string `json:"idValue"`
	// Properties are the values to set, keyed by internal property name. An
	// empty string clears a value.
	Properties map[string]string `json:"properties,omitempty"`
}

// UpsertedObject is the record an upsert created or updated.
type UpsertedObject struct {
	// Object is the record with the values HubSpot returned.
	Object CRMObject `json:"object"`
	// Created reports whether the dispatch HubSpot answered created the record.
	// After a Dex retry or a repeated dispatch it can be false even though an
	// earlier dispatch of the same Step created the record.
	Created bool `json:"created"`
}

// UpdateObjectInput replaces property values on one record.
type UpdateObjectInput struct {
	// ObjectType is the record's object type.
	ObjectType ObjectType `json:"objectType"`
	// ObjectID is HubSpot's decimal record ID.
	ObjectID string `json:"objectId"`
	// Properties are the values to set, keyed by internal property name. At
	// least one is required, and an empty string clears a value.
	Properties map[string]string `json:"properties"`
}

// SearchObjectsOperation implements the CRM search Query.
type SearchObjectsOperation struct{ client *Client }

// GetObjectOperation implements the record read Query.
type GetObjectOperation struct{ client *Client }

// UpsertObjectOperation implements the unique-property upsert Mutation.
type UpsertObjectOperation struct{ client *Client }

// UpdateObjectOperation implements the record update Mutation.
type UpdateObjectOperation struct{ client *Client }

type hubspotObject struct {
	ID         string             `json:"id"`
	Properties map[string]*string `json:"properties"`
	CreatedAt  time.Time          `json:"createdAt"`
	UpdatedAt  time.Time          `json:"updatedAt"`
	Archived   bool               `json:"archived"`
	URL        string             `json:"url"`
	New        bool               `json:"new"`
}

type hubspotSearchRequest struct {
	FilterGroups []SearchFilterGroup `json:"filterGroups,omitempty"`
	Query        string              `json:"query,omitempty"`
	Sorts        []SearchSort        `json:"sorts,omitempty"`
	Properties   []string            `json:"properties,omitempty"`
	Limit        int                 `json:"limit"`
	After        string              `json:"after,omitempty"`
}

type hubspotSearchResponse struct {
	Total   int             `json:"total"`
	Results []hubspotObject `json:"results"`
	Paging  *struct {
		Next *struct {
			After string `json:"after"`
		} `json:"next"`
	} `json:"paging"`
}

type hubspotUpsertInput struct {
	IDProperty string            `json:"idProperty"`
	ID         string            `json:"id"`
	Properties map[string]string `json:"properties"`
}

type hubspotUpsertRequest struct {
	Inputs []hubspotUpsertInput `json:"inputs"`
}

type hubspotBatchResponse struct {
	Results []hubspotObject `json:"results"`
	Errors  []struct {
		Status   string `json:"status"`
		Category string `json:"category"`
	} `json:"errors"`
}

type hubspotUpdateRequest struct {
	Properties map[string]string `json:"properties"`
}

var searchBranches = operationBranches{
	rejected: SearchObjectsBranchProviderRejected, invalidResponse: SearchObjectsBranchInvalidResponse, defect: SearchObjectsBranchDefect,
}

var getBranches = operationBranches{
	notFound: GetObjectBranchNotFound, rejected: GetObjectBranchProviderRejected,
	invalidResponse: GetObjectBranchInvalidResponse, defect: GetObjectBranchDefect,
}

var upsertBranches = operationBranches{
	conflict: UpsertObjectBranchConflict, rejected: UpsertObjectBranchProviderRejected,
	invalidResponse: UpsertObjectBranchInvalidResponse, defect: UpsertObjectBranchDefect,
}

var updateBranches = operationBranches{
	notFound: UpdateObjectBranchNotFound, conflict: UpdateObjectBranchConflict, rejected: UpdateObjectBranchProviderRejected,
	invalidResponse: UpdateObjectBranchInvalidResponse, defect: UpdateObjectBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (SearchObjectsOperation) Definition() sdkgo.QueryDefinition { return SearchObjectsDefinition }

// Invoke sends one CRM search request and returns one bounded page.
func (operation SearchObjectsOperation) Invoke(call sdkgo.Call, input SearchObjectsInput) sdkgo.QueryAttempt[ObjectPage] {
	const operationID = "searchObjects"
	body, err := input.searchRequestBody()
	if err != nil {
		return sdkgo.NewQueryBranch(SearchObjectsBranchDefect, ObjectPage{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	request := providerRequest{method: http.MethodPost, path: objectsPath(input.ObjectType, "search"), body: body}
	response, outcome := operation.client.exchange(call, operationID, request, "")
	if outcome != nil {
		return queryAttemptForOutcome[ObjectPage](*outcome, searchBranches)
	}
	receipt := operation.client.receipt(call, response, "")
	page, err := decodeObjectPage(input.ObjectType, response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchObjectsBranchInvalidResponse, ObjectPage{}, providerFailurePointer(operationID, sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchObjectsBranchSearched, page, nil, receipt)
}

// Definition returns the immutable connector operation definition.
func (GetObjectOperation) Definition() sdkgo.QueryDefinition { return GetObjectDefinition }

// Invoke reads one unarchived record by ID.
func (operation GetObjectOperation) Invoke(call sdkgo.Call, input GetObjectInput) sdkgo.QueryAttempt[CRMObject] {
	const operationID = "getObject"
	query, err := input.readQuery()
	if err != nil {
		return sdkgo.NewQueryBranch(GetObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	request := providerRequest{method: http.MethodGet, path: objectsPath(input.ObjectType, input.ObjectID), query: query}
	response, outcome := operation.client.exchange(call, operationID, request, input.ObjectID)
	if outcome != nil {
		return queryAttemptForOutcome[CRMObject](*outcome, getBranches)
	}
	receipt := operation.client.receipt(call, response, input.ObjectID)
	object, err := decodeObject(input.ObjectType, response.body)
	if err != nil || object.ID != input.ObjectID {
		return sdkgo.NewQueryBranch(GetObjectBranchInvalidResponse, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureProtocol, "HubSpot returned an invalid record"), receipt)
	}
	return sdkgo.NewQueryBranch(GetObjectBranchFound, object, nil, receipt)
}

// Definition returns the immutable connector operation definition.
func (UpsertObjectOperation) Definition() sdkgo.MutationDefinition { return UpsertObjectDefinition }

// IdempotencyKey derives the receipt key from the stable Call ID. HubSpot has
// no idempotency header; the unique IDProperty value makes the upsert idempotent.
func (UpsertObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates or updates the one record that owns the unique value. A
// conflict can come from a concurrent dispatch of the same upsert that created
// the record first, so the connector resends once, and the resend updates that
// record; a second conflict selects the conflict branch.
func (operation UpsertObjectOperation) Invoke(call sdkgo.Call, input UpsertObjectInput) sdkgo.MutationAttempt[UpsertedObject] {
	const operationID = "upsertObject"
	body, err := input.upsertRequestBody()
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertObjectBranchDefect, UpsertedObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	request := providerRequest{method: http.MethodPost, path: objectsPath(input.ObjectType, "batch/upsert"), body: body}
	upserted, receipt, outcome := operation.dispatchUpsert(call, operationID, input.ObjectType, request)
	if outcome != nil && outcome.disposition == outcomeConflict {
		upserted, receipt, outcome = operation.dispatchUpsert(call, operationID, input.ObjectType, request)
	}
	if outcome != nil {
		return mutationAttemptForOutcome[UpsertedObject](*outcome, upsertBranches)
	}
	return sdkgo.NewMutationBranch(UpsertObjectBranchUpserted, upserted, nil, receipt)
}

// dispatchUpsert sends one upsert and classifies its HTTP status, its batch error, and its record.
func (operation UpsertObjectOperation) dispatchUpsert(
	call sdkgo.Call,
	operationID string,
	objectType ObjectType,
	request providerRequest,
) (UpsertedObject, sdkgo.Receipt, *providerOutcome) {
	response, outcome := operation.client.exchange(call, operationID, request, "")
	if outcome != nil {
		return UpsertedObject{}, outcome.receipt, outcome
	}
	receipt := operation.client.receipt(call, response, "")
	upserted, batchOutcome, err := decodeUpsertResponse(operationID, objectType, response, operation.client.now())
	if batchOutcome != nil {
		batchOutcome.receipt = receipt
		return UpsertedObject{}, receipt, batchOutcome
	}
	if err != nil {
		return UpsertedObject{}, receipt, &providerOutcome{
			disposition: outcomeInvalidResponse, failure: providerFailure(operationID, sdkgo.FailureProtocol, err.Error()), receipt: receipt,
		}
	}
	receipt.ProviderObjectID = upserted.Object.ID
	return upserted, receipt, nil
}

// Definition returns the immutable connector operation definition.
func (UpdateObjectOperation) Definition() sdkgo.MutationDefinition { return UpdateObjectDefinition }

// IdempotencyKey derives the receipt key from the stable Call ID. HubSpot has
// no idempotency header; setting the same values again leaves the record unchanged.
func (UpdateObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the named property values on one record. Repeating the same
// update converges on the same values, so a failure after dispatch is retried.
func (operation UpdateObjectOperation) Invoke(call sdkgo.Call, input UpdateObjectInput) sdkgo.MutationAttempt[CRMObject] {
	const operationID = "updateObject"
	body, err := input.updateRequestBody()
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	request := providerRequest{method: http.MethodPatch, path: objectsPath(input.ObjectType, input.ObjectID), body: body}
	response, outcome := operation.client.exchange(call, operationID, request, input.ObjectID)
	if outcome != nil {
		return mutationAttemptForOutcome[CRMObject](*outcome, updateBranches)
	}
	receipt := operation.client.receipt(call, response, input.ObjectID)
	object, err := decodeObject(input.ObjectType, response.body)
	if err != nil || object.ID != input.ObjectID {
		return sdkgo.NewMutationBranch(UpdateObjectBranchInvalidResponse, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureProtocol, "HubSpot returned an invalid record"), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateObjectBranchUpdated, object, nil, receipt)
}

func (input SearchObjectsInput) searchRequestBody() ([]byte, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	if len(input.FilterGroups) > MaximumFilterGroups {
		return nil, fmt.Errorf("search allows at most %d filter groups", MaximumFilterGroups)
	}
	filterCount := 0
	for _, group := range input.FilterGroups {
		if len(group.Filters) == 0 || len(group.Filters) > MaximumFiltersPerGroup {
			return nil, fmt.Errorf("each filter group needs 1 to %d filters", MaximumFiltersPerGroup)
		}
		filterCount += len(group.Filters)
		for _, filter := range group.Filters {
			if err := filter.validate(); err != nil {
				return nil, err
			}
		}
	}
	if filterCount > MaximumFilters {
		return nil, fmt.Errorf("search allows at most %d filters in total", MaximumFilters)
	}
	properties, err := validatePropertyNames(input.Properties)
	if err != nil {
		return nil, err
	}
	request := hubspotSearchRequest{FilterGroups: input.FilterGroups, Query: input.Query, Properties: properties, Limit: input.Limit}
	if input.Sort != nil {
		if !propertyNamePattern.MatchString(input.Sort.PropertyName) {
			return nil, errors.New("sort property name is invalid")
		}
		if input.Sort.Direction != SortDirectionAscending && input.Sort.Direction != SortDirectionDescending {
			return nil, errors.New("sort direction must be ASCENDING or DESCENDING")
		}
		request.Sorts = []SearchSort{*input.Sort}
	}
	if request.Limit == 0 {
		request.Limit = DefaultSearchLimit
	}
	if request.Limit < 1 || request.Limit > MaximumSearchLimit {
		return nil, fmt.Errorf("search limit must be from 1 through %d", MaximumSearchLimit)
	}
	offset := 0
	if input.After != "" {
		if !searchCursorPattern.MatchString(input.After) {
			return nil, errors.New("search after cursor must be the decimal NextAfter value of a previous page")
		}
		offset, _ = strconv.Atoi(input.After)
		request.After = input.After
	}
	if offset+request.Limit > MaximumSearchResults {
		return nil, fmt.Errorf("search paging cannot pass HubSpot's %d-result limit", MaximumSearchResults)
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, errors.New("search request could not be encoded")
	}
	if len(body) > MaximumSearchRequestBytes {
		return nil, fmt.Errorf("search request exceeds HubSpot's %d-character body limit", MaximumSearchRequestBytes)
	}
	return body, nil
}

func (filter SearchFilter) validate() error {
	if !propertyNamePattern.MatchString(filter.PropertyName) && !associationPropertyPattern.MatchString(filter.PropertyName) {
		return errors.New("filter property name is invalid")
	}
	hasValue, hasHighValue, hasValues := filter.Value != "", filter.HighValue != "", len(filter.Values) > 0
	switch filter.Operator {
	case FilterOperatorEqual, FilterOperatorNotEqual, FilterOperatorLessThan, FilterOperatorLessThanOrEqual,
		FilterOperatorGreaterThan, FilterOperatorGreaterThanOrEqual, FilterOperatorContainsToken, FilterOperatorNotContainsToken:
		if !hasValue || hasHighValue || hasValues {
			return fmt.Errorf("filter operator %s needs only a value", filter.Operator)
		}
	case FilterOperatorBetween:
		if !hasValue || !hasHighValue || hasValues {
			return errors.New("filter operator BETWEEN needs a value and a highValue")
		}
	case FilterOperatorIn, FilterOperatorNotIn:
		if hasValue || hasHighValue || !hasValues || len(filter.Values) > MaximumFilterValues {
			return fmt.Errorf("filter operator %s needs 1 to %d values", filter.Operator, MaximumFilterValues)
		}
		for _, value := range filter.Values {
			if value == "" {
				return fmt.Errorf("filter operator %s values must be non-empty", filter.Operator)
			}
		}
	case FilterOperatorHasProperty, FilterOperatorNotHasProperty:
		if hasValue || hasHighValue || hasValues {
			return fmt.Errorf("filter operator %s takes no value", filter.Operator)
		}
	default:
		return errors.New("filter operator is not supported")
	}
	return nil
}

func (input GetObjectInput) readQuery() (url.Values, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	if !recordIDPattern.MatchString(input.ObjectID) {
		return nil, errors.New("object ID must be a decimal HubSpot record ID")
	}
	properties, err := validatePropertyNames(input.Properties)
	if err != nil {
		return nil, err
	}
	query := url.Values{"archived": {"false"}}
	if len(properties) > 0 {
		query.Set("properties", strings.Join(properties, ","))
	}
	return query, nil
}

func (input UpsertObjectInput) upsertRequestBody() ([]byte, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	if !propertyNamePattern.MatchString(input.IDProperty) {
		return nil, errors.New("upsert ID property name is invalid")
	}
	if strings.TrimSpace(input.IDValue) == "" || utf8.RuneCountInString(input.IDValue) > MaximumPropertyValueCharacters {
		return nil, errors.New("upsert ID value must be non-empty and within HubSpot's property value limit")
	}
	if err := validatePropertyValues(input.Properties, false); err != nil {
		return nil, err
	}
	properties := input.Properties
	if properties == nil {
		properties = map[string]string{}
	}
	body, err := json.Marshal(hubspotUpsertRequest{Inputs: []hubspotUpsertInput{{
		IDProperty: input.IDProperty, ID: input.IDValue, Properties: properties,
	}}})
	if err != nil {
		return nil, errors.New("upsert request could not be encoded")
	}
	return body, nil
}

func (input UpdateObjectInput) updateRequestBody() ([]byte, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	if !recordIDPattern.MatchString(input.ObjectID) {
		return nil, errors.New("object ID must be a decimal HubSpot record ID")
	}
	if err := validatePropertyValues(input.Properties, true); err != nil {
		return nil, err
	}
	body, err := json.Marshal(hubspotUpdateRequest{Properties: input.Properties})
	if err != nil {
		return nil, errors.New("update request could not be encoded")
	}
	return body, nil
}

func (objectType ObjectType) validate() error {
	switch objectType {
	case ObjectTypeContacts, ObjectTypeCompanies, ObjectTypeDeals:
		return nil
	default:
		return errors.New("object type must be contacts, companies, or deals")
	}
}

// validatePropertyNames returns the names without duplicates, in their first order.
func validatePropertyNames(names []string) ([]string, error) {
	if len(names) > MaximumProperties {
		return nil, fmt.Errorf("at most %d properties can be requested", MaximumProperties)
	}
	seen := make(map[string]bool, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if !propertyNamePattern.MatchString(name) {
			return nil, errors.New("requested property name is invalid")
		}
		if !seen[name] {
			seen[name] = true
			unique = append(unique, name)
		}
	}
	return unique, nil
}

func validatePropertyValues(properties map[string]string, isRequired bool) error {
	if isRequired && len(properties) == 0 {
		return errors.New("at least one property value is required")
	}
	if len(properties) > MaximumProperties {
		return fmt.Errorf("at most %d property values can be written", MaximumProperties)
	}
	for name, value := range properties {
		if !propertyNamePattern.MatchString(name) {
			return errors.New("written property name is invalid")
		}
		if utf8.RuneCountInString(value) > MaximumPropertyValueCharacters {
			return fmt.Errorf("property values cannot exceed %d characters", MaximumPropertyValueCharacters)
		}
	}
	return nil
}

func objectsPath(objectType ObjectType, suffix string) string {
	return "crm/objects/" + crmAPIVersion + "/" + string(objectType) + "/" + suffix
}

func decodeObjectPage(objectType ObjectType, body []byte) (ObjectPage, error) {
	var decoded hubspotSearchResponse
	if err := json.Unmarshal(body, &decoded); err != nil || decoded.Total < 0 {
		return ObjectPage{}, errResponseMalformed
	}
	page := ObjectPage{ObjectType: objectType, Total: decoded.Total, Objects: make([]CRMObject, 0, len(decoded.Results))}
	for _, result := range decoded.Results {
		object, err := result.crmObject(objectType)
		if err != nil {
			return ObjectPage{}, err
		}
		page.Objects = append(page.Objects, object)
	}
	if decoded.Paging != nil && decoded.Paging.Next != nil && decoded.Paging.Next.After != "" {
		if !searchCursorPattern.MatchString(decoded.Paging.Next.After) {
			return ObjectPage{}, errors.New("HubSpot returned an invalid search cursor")
		}
		page.NextAfter = decoded.Paging.Next.After
	}
	return page, nil
}

func decodeObject(objectType ObjectType, body []byte) (CRMObject, error) {
	var decoded hubspotObject
	if err := json.Unmarshal(body, &decoded); err != nil {
		return CRMObject{}, errResponseMalformed
	}
	return decoded.crmObject(objectType)
}

// decodeUpsertResponse returns the one upserted record, or the outcome of the input's reported error.
func decodeUpsertResponse(operation string, objectType ObjectType, response providerResponse, now time.Time) (UpsertedObject, *providerOutcome, error) {
	var decoded hubspotBatchResponse
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		return UpsertedObject{}, nil, errResponseMalformed
	}
	if len(decoded.Results) == 0 && len(decoded.Errors) > 0 {
		errorStatus, err := strconv.Atoi(decoded.Errors[0].Status)
		if err != nil || errorStatus < 400 {
			errorStatus = http.StatusBadRequest
		}
		categoryBody, _ := json.Marshal(map[string]string{"category": decoded.Errors[0].Category})
		outcome := classifyErrorResponse(operation, providerResponse{statusCode: errorStatus, header: response.header, body: categoryBody}, now)
		return UpsertedObject{}, &outcome, nil
	}
	switch len(decoded.Results) {
	case 0:
		return UpsertedObject{}, nil, errNoRecordInUpsertResponse
	case 1:
	default:
		return UpsertedObject{}, nil, errManyRecordsInUpsertResult
	}
	object, err := decoded.Results[0].crmObject(objectType)
	if err != nil {
		return UpsertedObject{}, nil, err
	}
	return UpsertedObject{Object: object, Created: decoded.Results[0].New}, nil, nil
}

func (object hubspotObject) crmObject(objectType ObjectType) (CRMObject, error) {
	if !recordIDPattern.MatchString(object.ID) {
		return CRMObject{}, errors.New("HubSpot returned a record without a valid ID")
	}
	converted := CRMObject{
		ObjectType: objectType, ID: object.ID, CreatedAt: object.CreatedAt, UpdatedAt: object.UpdatedAt,
		Archived: object.Archived, URL: hubspotAppURL(object.URL),
	}
	for name, value := range object.Properties {
		if value == nil {
			continue
		}
		if converted.Properties == nil {
			converted.Properties = make(map[string]string, len(object.Properties))
		}
		converted.Properties[name] = *value
	}
	return converted, nil
}

// hubspotAppURL keeps only an HTTPS link into HubSpot's own app.
func hubspotAppURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || !strings.HasSuffix(parsed.Hostname(), ".hubspot.com") {
		return ""
	}
	return parsed.String()
}
