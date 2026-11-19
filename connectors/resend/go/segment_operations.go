// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateSegmentInput creates a named contact segment.
type CreateSegmentInput struct {
	// Name is the segment name.
	Name string `json:"name"`
}

// GetSegmentInput selects a segment.
type GetSegmentInput struct {
	// ID is the segment ID.
	ID string `json:"id"`
}

// ListSegmentsInput selects a bounded segment page.
type ListSegmentsInput struct {
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive segment ID cursor.
	After string `json:"after,omitempty"`
}

// UpdateSegmentInput renames a segment.
type UpdateSegmentInput struct {
	// ID is the segment ID.
	ID string `json:"id"`
	// Name is the replacement segment name.
	Name string `json:"name"`
}

// DeleteSegmentInput selects a segment for deletion.
type DeleteSegmentInput struct {
	// ID is the segment ID.
	ID string `json:"id"`
}

// ListSegmentContactsInput selects contacts belonging to a segment.
type ListSegmentContactsInput struct {
	// SegmentID is the segment ID.
	SegmentID string `json:"segmentId"`
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive contact ID cursor.
	After string `json:"after,omitempty"`
}

// SegmentReference identifies a created or updated segment.
type SegmentReference struct {
	// ID is the stable provider segment ID.
	ID string `json:"id"`
}

// Segment contains selected segment fields.
type Segment struct {
	// ID is the stable provider segment ID.
	ID string `json:"id"`
	// Name is the segment name.
	Name string `json:"name"`
}

// SegmentPage contains one segment page.
type SegmentPage struct {
	// Data contains segment summaries.
	Data []Segment `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedSegment records a provider deletion acknowledgement.
type DeletedSegment struct {
	// ID is the deleted segment ID.
	ID string `json:"id"`
	// Deleted confirms that deletion succeeded.
	Deleted bool `json:"deleted"`
}

// CreateSegmentOperation creates a segment with bounded resend.
type CreateSegmentOperation struct{ client *Client }

// GetSegmentOperation retrieves one segment.
type GetSegmentOperation struct{ client *Client }

// ListSegmentsOperation lists segments.
type ListSegmentsOperation struct{ client *Client }

// UpdateSegmentOperation renames a segment.
type UpdateSegmentOperation struct{ client *Client }

// DeleteSegmentOperation removes a segment.
type DeleteSegmentOperation struct{ client *Client }

// ListSegmentContactsOperation lists contacts in a segment.
type ListSegmentContactsOperation struct{ client *Client }

// CreateSegment binds segment creation to this client.
func (client *Client) CreateSegment() CreateSegmentOperation { return CreateSegmentOperation{client} }

// GetSegment binds segment retrieval to this client.
func (client *Client) GetSegment() GetSegmentOperation { return GetSegmentOperation{client} }

// ListSegments binds segment listing to this client.
func (client *Client) ListSegments() ListSegmentsOperation { return ListSegmentsOperation{client} }

// UpdateSegment binds segment renaming to this client.
func (client *Client) UpdateSegment() UpdateSegmentOperation { return UpdateSegmentOperation{client} }

// DeleteSegment binds segment deletion to this client.
func (client *Client) DeleteSegment() DeleteSegmentOperation { return DeleteSegmentOperation{client} }

// ListSegmentContacts binds contact listing to this client.
func (client *Client) ListSegmentContacts() ListSegmentContactsOperation {
	return ListSegmentContactsOperation{client}
}

// Definition returns the generated createSegment contract.
func (CreateSegmentOperation) Definition() sdkgo.MutationDefinition { return CreateSegmentDefinition }

// Definition returns the generated getSegment contract.
func (GetSegmentOperation) Definition() sdkgo.QueryDefinition { return GetSegmentDefinition }

// Definition returns the generated listSegments contract.
func (ListSegmentsOperation) Definition() sdkgo.QueryDefinition { return ListSegmentsDefinition }

// Definition returns the generated updateSegment contract.
func (UpdateSegmentOperation) Definition() sdkgo.MutationDefinition { return UpdateSegmentDefinition }

// Definition returns the generated deleteSegment contract.
func (DeleteSegmentOperation) Definition() sdkgo.MutationDefinition { return DeleteSegmentDefinition }

// Definition returns the generated listSegmentContacts contract.
func (ListSegmentContactsOperation) Definition() sdkgo.QueryDefinition {
	return ListSegmentContactsDefinition
}

// IdempotencyKey correlates creation attempts without asserting provider deduplication.
func (CreateSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute segment renames.
func (UpdateSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a segment and confirms possible earlier sends by name and filter.
func (operation CreateSegmentOperation) Invoke(call sdkgo.Call, input CreateSegmentInput) sdkgo.MutationAttempt[SegmentReference] {
	if strings.TrimSpace(input.Name) == "" {
		return mutationFailure[SegmentReference]("createSegment", CreateSegmentBranchDefect, sdkgo.FailureValidation, "segment name is required")
	}
	body, err := json.Marshal(map[string]string{"name": input.Name})
	if err != nil {
		return mutationFailure[SegmentReference]("createSegment", CreateSegmentBranchDefect, sdkgo.FailureValidation, "segment cannot be encoded")
	}
	request := resendLimitedCreate[SegmentReference]{client: operation.client, call: call, operation: "createSegment", path: "/segments", body: body, timeout: CreateSegmentDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateSegmentBranchCreated, rejected: CreateSegmentBranchProviderRejected, defect: CreateSegmentBranchDefect, decode: decodeSegmentReference, readBack: func(call sdkgo.Call) (SegmentReference, readBackState) { return operation.readBackSegment(call, input) }}
	return request.invoke()
}

// Invoke retrieves a segment by ID.
func (operation GetSegmentOperation) Invoke(call sdkgo.Call, input GetSegmentInput) sdkgo.QueryAttempt[Segment] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[Segment]("getSegment", GetSegmentBranchDefect, sdkgo.FailureValidation, "segment ID is required")
	}
	return runQuery(operation.client, call, "getSegment", "/segments/"+url.PathEscape(input.ID), GetSegmentBranchFound, GetSegmentBranchNotFound, GetSegmentBranchProviderRejected, GetSegmentBranchInvalidResponse, GetSegmentBranchDefect, decodeSegment)
}

// Invoke lists one segment page.
func (operation ListSegmentsOperation) Invoke(call sdkgo.Call, input ListSegmentsInput) sdkgo.QueryAttempt[SegmentPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[SegmentPage]("listSegments", ListSegmentsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listSegments", "/segments"+query, ListSegmentsBranchListed, "", ListSegmentsBranchProviderRejected, ListSegmentsBranchInvalidResponse, ListSegmentsBranchDefect, decodeSegmentPage)
}

// Invoke replaces a segment name.
func (operation UpdateSegmentOperation) Invoke(call sdkgo.Call, input UpdateSegmentInput) sdkgo.MutationAttempt[SegmentReference] {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Name) == "" {
		return mutationFailure[SegmentReference]("updateSegment", UpdateSegmentBranchDefect, sdkgo.FailureValidation, "segment ID and replacement name are required")
	}
	body, err := json.Marshal(map[string]string{"name": input.Name})
	if err != nil {
		return mutationFailure[SegmentReference]("updateSegment", UpdateSegmentBranchDefect, sdkgo.FailureValidation, "segment cannot be encoded")
	}
	return runMutation(operation.client, call, "updateSegment", http.MethodPatch, "/segments/"+url.PathEscape(input.ID), body, "", "", UpdateSegmentBranchUpdated, UpdateSegmentBranchNotFound, UpdateSegmentBranchProviderRejected, UpdateSegmentBranchInvalidResponse, UpdateSegmentBranchDefect, decodeSegmentReference)
}

// Invoke deletes a segment by ID.
func (operation DeleteSegmentOperation) Invoke(call sdkgo.Call, input DeleteSegmentInput) sdkgo.MutationAttempt[DeletedSegment] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedSegment]("deleteSegment", DeleteSegmentBranchDefect, sdkgo.FailureValidation, "segment ID is required")
	}
	return runMutation(operation.client, call, "deleteSegment", http.MethodDelete, "/segments/"+url.PathEscape(input.ID), nil, "", "", DeleteSegmentBranchDeleted, DeleteSegmentBranchNotFound, DeleteSegmentBranchProviderRejected, DeleteSegmentBranchInvalidResponse, DeleteSegmentBranchDefect, decodeDeletedSegment)
}

// Invoke lists contacts belonging to a segment.
func (operation ListSegmentContactsOperation) Invoke(call sdkgo.Call, input ListSegmentContactsInput) sdkgo.QueryAttempt[ContactPage] {
	if strings.TrimSpace(input.SegmentID) == "" {
		return queryFailure[ContactPage]("listSegmentContacts", ListSegmentContactsBranchDefect, sdkgo.FailureValidation, "segment ID is required")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[ContactPage]("listSegmentContacts", ListSegmentContactsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listSegmentContacts", "/segments/"+url.PathEscape(input.SegmentID)+"/contacts"+query, ListSegmentContactsBranchListed, "", ListSegmentContactsBranchProviderRejected, ListSegmentContactsBranchInvalidResponse, ListSegmentContactsBranchDefect, func(body []byte) (ContactPage, error) {
		var page ContactPage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Data == nil {
			err = fmt.Errorf("segment contact page data missing")
		}
		return page, err
	})
}

func (operation CreateSegmentOperation) readBackSegment(call sdkgo.Call, input CreateSegmentInput) (SegmentReference, readBackState) {
	cursor := ""
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query, _ := listQuery(100, cursor, false)
		body, state := operation.client.readBackResource(call, "/segments"+query)
		if state != readBackMatched {
			return SegmentReference{}, readBackRetry
		}
		page, err := decodeSegmentPage(body)
		if err != nil {
			return SegmentReference{}, readBackRetry
		}
		for _, segment := range page.Data {
			if strings.EqualFold(segment.Name, input.Name) {
				return SegmentReference{ID: segment.ID}, readBackMatched
			}
		}
		if !page.HasMore {
			return SegmentReference{}, readBackAbsent
		}
		if len(page.Data) == 0 || page.Data[len(page.Data)-1].ID == cursor {
			return SegmentReference{}, readBackRetry
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return SegmentReference{}, readBackRetry
}
func decodeSegmentReference(body []byte) (SegmentReference, error) {
	var value SegmentReference
	err := json.Unmarshal(body, &value)
	if err == nil && value.ID == "" {
		err = fmt.Errorf("segment ID missing")
	}
	return value, err
}
func decodeSegment(body []byte) (Segment, error) {
	var value Segment
	err := json.Unmarshal(body, &value)
	if err == nil && (value.ID == "" || value.Name == "") {
		err = fmt.Errorf("segment fields missing")
	}
	return value, err
}
func decodeSegmentPage(body []byte) (SegmentPage, error) {
	var page SegmentPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("segment page data missing")
	}
	return page, err
}
func decodeDeletedSegment(body []byte) (DeletedSegment, error) {
	var value DeletedSegment
	err := json.Unmarshal(body, &value)
	if err == nil && (!value.Deleted || value.ID == "") {
		err = fmt.Errorf("segment deletion acknowledgement missing")
	}
	return value, err
}
