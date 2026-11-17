// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// SalesActivityMetadata contains the provider identity and scheduled time range.
type SalesActivityMetadata struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

// SalesActivityPage contains one page and the provider total.
type SalesActivityPage struct {
	Activities []SalesActivityMetadata `json:"activities"`
	Total      int                     `json:"total"`
}

// GetSalesActivityInput identifies one sales activity.
type GetSalesActivityInput struct {
	SalesActivityID string `json:"salesActivityId"`
}

// ListSalesActivitiesInput selects a one-based page.
type ListSalesActivitiesInput struct {
	Page int `json:"page,omitempty"`
}

// CreateSalesActivityInput schedules a titled sales activity for a CRM record.
type CreateSalesActivityInput struct {
	Title          string `json:"title"`
	StartDate      string `json:"startDate"`
	EndDate        string `json:"endDate"`
	TargetType     string `json:"targetType"`
	TargetID       string `json:"targetId"`
	OwnerID        string `json:"ownerId"`
	ActivityTypeID string `json:"activityTypeId"`
}

// UpdateSalesActivityInput replaces a sales activity title.
type UpdateSalesActivityInput struct {
	SalesActivityID string `json:"salesActivityId"`
	Title           string `json:"title"`
}

// DeleteSalesActivityInput identifies a sales activity to delete.
type DeleteSalesActivityInput struct {
	SalesActivityID string `json:"salesActivityId"`
}

// GetSalesActivityOperation reads a sales activity.
type GetSalesActivityOperation struct{ client *Client }

// ListSalesActivitiesOperation lists filtered salesActivities.
type ListSalesActivitiesOperation struct{ client *Client }

// CreateSalesActivityOperation schedules a sales activity with bounded sends.
type CreateSalesActivityOperation struct{ client *Client }

// UpdateSalesActivityOperation replaces a sales activity title.
type UpdateSalesActivityOperation struct{ client *Client }

// DeleteSalesActivityOperation deletes a sales activity.
type DeleteSalesActivityOperation struct{ client *Client }

// GetSalesActivity binds the query to this client.
func (client *Client) GetSalesActivity() GetSalesActivityOperation {
	return GetSalesActivityOperation{client}
}

// ListSalesActivities binds the query to this client.
func (client *Client) ListSalesActivities() ListSalesActivitiesOperation {
	return ListSalesActivitiesOperation{client}
}

// CreateSalesActivity binds the mutation to this client.
func (client *Client) CreateSalesActivity() CreateSalesActivityOperation {
	return CreateSalesActivityOperation{client}
}

// UpdateSalesActivity binds the mutation to this client.
func (client *Client) UpdateSalesActivity() UpdateSalesActivityOperation {
	return UpdateSalesActivityOperation{client}
}

// DeleteSalesActivity binds the mutation to this client.
func (client *Client) DeleteSalesActivity() DeleteSalesActivityOperation {
	return DeleteSalesActivityOperation{client}
}

// Definition returns the manifest-generated operation contract.
func (GetSalesActivityOperation) Definition() sdkgo.QueryDefinition {
	return GetSalesActivityDefinition
}

// Definition returns the manifest-generated operation contract.
func (ListSalesActivitiesOperation) Definition() sdkgo.QueryDefinition {
	return ListSalesActivitiesDefinition
}

// Definition returns the manifest-generated operation contract.
func (CreateSalesActivityOperation) Definition() sdkgo.MutationDefinition {
	return CreateSalesActivityDefinition
}

// Definition returns the manifest-generated operation contract.
func (UpdateSalesActivityOperation) Definition() sdkgo.MutationDefinition {
	return UpdateSalesActivityDefinition
}

// Definition returns the manifest-generated operation contract.
func (DeleteSalesActivityOperation) Definition() sdkgo.MutationDefinition {
	return DeleteSalesActivityDefinition
}

// IdempotencyKey correlates a call; Freshsales does not deduplicate sales activity creation.
func (CreateSalesActivityOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSalesActivityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a call; title replacement safely repeats.
func (UpdateSalesActivityOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSalesActivityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a call; deletion safely repeats.
func (DeleteSalesActivityOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSalesActivityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one sales activity by ID.
func (operation GetSalesActivityOperation) Invoke(call sdkgo.Call, input GetSalesActivityInput) sdkgo.QueryAttempt[SalesActivityMetadata] {
	branches := queryBranches{found: GetSalesActivityBranchFound, notFound: GetSalesActivityBranchNotFound, rejected: GetSalesActivityBranchProviderRejected, invalid: GetSalesActivityBranchInvalidResponse, defect: GetSalesActivityBranchDefect}
	if !isPositiveContactID(input.SalesActivityID) {
		return queryBranch("getSalesActivity", branches.defect, SalesActivityMetadata{}, sdkgo.FailureValidation, "sales activity ID is invalid")
	}
	return invokeQuery(call, operation.client, "getSalesActivity", "/sales_activities/"+input.SalesActivityID, branches, decodeSalesActivity)
}

// Invoke lists one page of sales activities.
func (operation ListSalesActivitiesOperation) Invoke(call sdkgo.Call, input ListSalesActivitiesInput) sdkgo.QueryAttempt[SalesActivityPage] {
	branches := queryBranches{found: ListSalesActivitiesBranchListed, rejected: ListSalesActivitiesBranchProviderRejected, invalid: ListSalesActivitiesBranchInvalidResponse, defect: ListSalesActivitiesBranchDefect}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.Page < 1 || input.Page > 10000 {
		return queryBranch("listSalesActivities", branches.defect, SalesActivityPage{}, sdkgo.FailureValidation, "page is invalid")
	}
	path := "/sales_activities?page=" + strconv.Itoa(input.Page)
	return invokeQuery(call, operation.client, "listSalesActivities", path, branches, decodeSalesActivitiesPage)
}

// Invoke creates a sales activity with bounded possible sends.
func (operation CreateSalesActivityOperation) Invoke(call sdkgo.Call, input CreateSalesActivityInput) sdkgo.MutationAttempt[SalesActivityMetadata] {
	if validateCRMName(input.Title) != nil || !validSalesActivityTimes(input.StartDate, input.EndDate) || !validTargetType(input.TargetType) || !isPositiveContactID(input.TargetID) || !isPositiveContactID(input.OwnerID) || !isPositiveContactID(input.ActivityTypeID) {
		return createBranch[SalesActivityMetadata]("createSalesActivity", CreateSalesActivityBranchDefect, sdkgo.FailureValidation, "sales activity title, time range, target, owner, or type is invalid")
	}
	targetID, _ := strconv.ParseInt(input.TargetID, 10, 64)
	ownerID, _ := strconv.ParseInt(input.OwnerID, 10, 64)
	activityTypeID, _ := strconv.ParseInt(input.ActivityTypeID, 10, 64)
	body, err := json.Marshal(map[string]any{"sales_activity": map[string]any{"title": input.Title, "start_date": input.StartDate, "end_date": input.EndDate, "targetable_type": input.TargetType, "targetable_id": targetID, "owner_id": ownerID, "sales_activity_type_id": activityTypeID}})
	if err != nil {
		return createBranch[SalesActivityMetadata]("createSalesActivity", CreateSalesActivityBranchDefect, sdkgo.FailureLocalDefect, "sales activity request cannot be encoded")
	}
	return invokeLimitedCreate(operation.client, call, "createSalesActivity", "/sales_activities", body, CreateSalesActivityDefinition.StepDefaults.ExecuteMethodTimeout, createBranches{CreateSalesActivityBranchCreated, CreateSalesActivityBranchProviderRejected, CreateSalesActivityBranchDefect}, decodeSalesActivity, func(value SalesActivityMetadata) string { return strconv.FormatInt(value.ID, 10) }, func(id string) string { return "/sales_activities/" + id })
}

// Invoke replaces the title and confirms the sales activity by read-back.
func (operation UpdateSalesActivityOperation) Invoke(call sdkgo.Call, input UpdateSalesActivityInput) sdkgo.MutationAttempt[SalesActivityMetadata] {
	if !isPositiveContactID(input.SalesActivityID) || validateCRMName(input.Title) != nil {
		return createBranch[SalesActivityMetadata]("updateSalesActivity", UpdateSalesActivityBranchDefect, sdkgo.FailureValidation, "sales activity ID or title is invalid")
	}
	path := "/sales_activities/" + input.SalesActivityID
	body, status, delay, err := operation.client.requestJSON(call, call.Context, http.MethodPut, path, map[string]any{"sales_activity": map[string]string{"title": input.Title}})
	if attempt := classifySalesActivityUpdate("updateSalesActivity", status, delay, err); attempt != nil {
		return *attempt
	}
	updated, decodeErr := decodeSalesActivity(body)
	if decodeErr != nil || strconv.FormatInt(updated.ID, 10) != input.SalesActivityID {
		return createRetry[SalesActivityMetadata]("updateSalesActivity", sdkgo.FailureProtocol, "sales activity update answer unconfirmed", 0)
	}
	confirmedBody, status, delay, err := operation.client.get(call, path)
	if attempt := classifySalesActivityUpdate("updateSalesActivity", status, delay, err); attempt != nil {
		return *attempt
	}
	confirmed, decodeErr := decodeSalesActivity(confirmedBody)
	if decodeErr != nil || confirmed.ID != updated.ID || confirmed.Title != input.Title {
		return createRetry[SalesActivityMetadata]("updateSalesActivity", sdkgo.FailureProtocol, "updated sales activity not yet confirmed", 0)
	}
	return sdkgo.NewMutationBranch(UpdateSalesActivityBranchUpdated, confirmed, nil, createReceipt(call, input.SalesActivityID))
}

// Invoke deletes one sales activity by ID.
func (operation DeleteSalesActivityOperation) Invoke(call sdkgo.Call, input DeleteSalesActivityInput) sdkgo.MutationAttempt[DeletedCRMRecord] {
	if !isPositiveContactID(input.SalesActivityID) {
		return deletedNamedBranch("deleteSalesActivity", DeleteSalesActivityBranchDefect, sdkgo.FailureValidation, "sales activity ID is invalid")
	}
	id, _ := strconv.ParseInt(input.SalesActivityID, 10, 64)
	return invokeNamedDelete(operation.client, call, "deleteSalesActivity", "/sales_activities/"+input.SalesActivityID, id, DeleteSalesActivityBranchDeleted, DeleteSalesActivityBranchProviderRejected, DeleteSalesActivityBranchDefect)
}

func classifySalesActivityUpdate(operation string, status int, delay time.Duration, err error) *sdkgo.MutationAttempt[SalesActivityMetadata] {
	var attempt sdkgo.MutationAttempt[SalesActivityMetadata]
	switch {
	case errors.Is(err, errCredential):
		attempt = createBranch[SalesActivityMetadata](operation, UpdateSalesActivityBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable or invalid")
	case status == http.StatusTooManyRequests:
		attempt = createRetry[SalesActivityMetadata](operation, sdkgo.FailureRateLimit, "Freshsales rate limited sales activity update", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		attempt = createRetry[SalesActivityMetadata](operation, sdkgo.FailureAvailability, "sales activity update outcome unconfirmed", delay)
	case status == http.StatusNotFound:
		attempt = createBranch[SalesActivityMetadata](operation, UpdateSalesActivityBranchNotFound, sdkgo.FailureProviderRejection, "sales activity does not exist")
	case status != http.StatusOK:
		attempt = createBranch[SalesActivityMetadata](operation, UpdateSalesActivityBranchProviderRejected, sdkgo.FailureProviderRejection, "Freshsales rejected HTTP "+strconv.Itoa(status))
	case err != nil:
		attempt = createRetry[SalesActivityMetadata](operation, sdkgo.FailureTransport, "sales activity response interrupted", 0)
	default:
		return nil
	}
	return &attempt
}

func validSalesActivityTimes(startDate, endDate string) bool {
	from, fromErr := time.Parse(time.RFC3339, startDate)
	end, endErr := time.Parse(time.RFC3339, endDate)
	return fromErr == nil && endErr == nil && end.After(from)
}

func decodeSalesActivity(body []byte) (SalesActivityMetadata, error) {
	var envelope struct {
		SalesActivity struct {
			ID        int64  `json:"id"`
			Title     string `json:"title"`
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"sales_activity"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return SalesActivityMetadata{}, err
	}
	value := SalesActivityMetadata{ID: envelope.SalesActivity.ID, Title: envelope.SalesActivity.Title, StartDate: envelope.SalesActivity.StartDate, EndDate: envelope.SalesActivity.EndDate}
	if value.ID <= 0 || value.Title == "" {
		return SalesActivityMetadata{}, errors.New("sales activity identity or title missing")
	}
	return value, nil
}

func decodeSalesActivitiesPage(body []byte) (SalesActivityPage, error) {
	var envelope struct {
		SalesActivities []struct {
			ID        int64  `json:"id"`
			Title     string `json:"title"`
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"sales_activities"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return SalesActivityPage{}, err
	}
	if envelope.SalesActivities == nil || envelope.Meta.Total < 0 {
		return SalesActivityPage{}, errors.New("sales activity page missing")
	}
	result := SalesActivityPage{Activities: make([]SalesActivityMetadata, 0, len(envelope.SalesActivities)), Total: envelope.Meta.Total}
	for _, item := range envelope.SalesActivities {
		if item.ID <= 0 || item.Title == "" {
			return SalesActivityPage{}, errors.New("sales activity item invalid")
		}
		result.Activities = append(result.Activities, SalesActivityMetadata{ID: item.ID, Title: item.Title, StartDate: item.StartDate, EndDate: item.EndDate})
	}
	return result, nil
}
