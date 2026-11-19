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

// AppointmentMetadata contains the provider identity and scheduled time range.
type AppointmentMetadata struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	FromDate string `json:"fromDate"`
	EndDate  string `json:"endDate"`
}

// AppointmentPage contains one filtered page and the provider total.
type AppointmentPage struct {
	Appointments []AppointmentMetadata `json:"appointments"`
	Total        int                   `json:"total"`
}

// GetAppointmentInput identifies one appointment.
type GetAppointmentInput struct {
	AppointmentID string `json:"appointmentId"`
}

// ListAppointmentsInput selects open or completed appointments and a one-based page.
type ListAppointmentsInput struct {
	Filter string `json:"filter"`
	Page   int    `json:"page,omitempty"`
}

// CreateAppointmentInput schedules a titled appointment for a CRM record.
type CreateAppointmentInput struct {
	Title      string `json:"title"`
	FromDate   string `json:"fromDate"`
	EndDate    string `json:"endDate"`
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
}

// UpdateAppointmentInput replaces an appointment title.
type UpdateAppointmentInput struct {
	AppointmentID string `json:"appointmentId"`
	Title         string `json:"title"`
}

// DeleteAppointmentInput identifies an appointment to delete.
type DeleteAppointmentInput struct {
	AppointmentID string `json:"appointmentId"`
}

// GetAppointmentOperation reads an appointment.
type GetAppointmentOperation struct{ client *Client }

// ListAppointmentsOperation lists filtered appointments.
type ListAppointmentsOperation struct{ client *Client }

// CreateAppointmentOperation schedules an appointment with bounded sends.
type CreateAppointmentOperation struct{ client *Client }

// UpdateAppointmentOperation replaces an appointment title.
type UpdateAppointmentOperation struct{ client *Client }

// DeleteAppointmentOperation deletes an appointment.
type DeleteAppointmentOperation struct{ client *Client }

// GetAppointment binds the query to this client.
func (client *Client) GetAppointment() GetAppointmentOperation {
	return GetAppointmentOperation{client}
}

// ListAppointments binds the query to this client.
func (client *Client) ListAppointments() ListAppointmentsOperation {
	return ListAppointmentsOperation{client}
}

// CreateAppointment binds the mutation to this client.
func (client *Client) CreateAppointment() CreateAppointmentOperation {
	return CreateAppointmentOperation{client}
}

// UpdateAppointment binds the mutation to this client.
func (client *Client) UpdateAppointment() UpdateAppointmentOperation {
	return UpdateAppointmentOperation{client}
}

// DeleteAppointment binds the mutation to this client.
func (client *Client) DeleteAppointment() DeleteAppointmentOperation {
	return DeleteAppointmentOperation{client}
}

// Definition returns the manifest-generated operation contract.
func (GetAppointmentOperation) Definition() sdkgo.QueryDefinition { return GetAppointmentDefinition }

// Definition returns the manifest-generated operation contract.
func (ListAppointmentsOperation) Definition() sdkgo.QueryDefinition {
	return ListAppointmentsDefinition
}

// Definition returns the manifest-generated operation contract.
func (CreateAppointmentOperation) Definition() sdkgo.MutationDefinition {
	return CreateAppointmentDefinition
}

// Definition returns the manifest-generated operation contract.
func (UpdateAppointmentOperation) Definition() sdkgo.MutationDefinition {
	return UpdateAppointmentDefinition
}

// Definition returns the manifest-generated operation contract.
func (DeleteAppointmentOperation) Definition() sdkgo.MutationDefinition {
	return DeleteAppointmentDefinition
}

// IdempotencyKey correlates a call; Freshsales does not deduplicate appointment creation.
func (CreateAppointmentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAppointmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a call; title replacement safely repeats.
func (UpdateAppointmentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAppointmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a call; deletion safely repeats.
func (DeleteAppointmentOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAppointmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one appointment by ID.
func (operation GetAppointmentOperation) Invoke(call sdkgo.Call, input GetAppointmentInput) sdkgo.QueryAttempt[AppointmentMetadata] {
	branches := queryBranches{found: GetAppointmentBranchFound, notFound: GetAppointmentBranchNotFound, rejected: GetAppointmentBranchProviderRejected, invalid: GetAppointmentBranchInvalidResponse, defect: GetAppointmentBranchDefect}
	if !isPositiveContactID(input.AppointmentID) {
		return queryBranch("getAppointment", branches.defect, AppointmentMetadata{}, sdkgo.FailureValidation, "appointment ID is invalid")
	}
	return invokeQuery(call, operation.client, "getAppointment", "/appointments/"+input.AppointmentID, branches, decodeAppointment)
}

// Invoke lists a filtered page of appointments.
func (operation ListAppointmentsOperation) Invoke(call sdkgo.Call, input ListAppointmentsInput) sdkgo.QueryAttempt[AppointmentPage] {
	branches := queryBranches{found: ListAppointmentsBranchListed, rejected: ListAppointmentsBranchProviderRejected, invalid: ListAppointmentsBranchInvalidResponse, defect: ListAppointmentsBranchDefect}
	if input.Page == 0 {
		input.Page = 1
	}
	if (input.Filter != "open" && input.Filter != "complete") || input.Page < 1 || input.Page > 10000 {
		return queryBranch("listAppointments", branches.defect, AppointmentPage{}, sdkgo.FailureValidation, "filter or page is invalid")
	}
	path := "/appointments?filter=" + input.Filter + "&page=" + strconv.Itoa(input.Page)
	return invokeQuery(call, operation.client, "listAppointments", path, branches, decodeAppointmentsPage)
}

// Invoke creates an appointment with bounded possible sends.
func (operation CreateAppointmentOperation) Invoke(call sdkgo.Call, input CreateAppointmentInput) sdkgo.MutationAttempt[AppointmentMetadata] {
	if validateCRMName(input.Title) != nil || !validAppointmentTimes(input.FromDate, input.EndDate) || !validTargetType(input.TargetType) || !isPositiveContactID(input.TargetID) {
		return createBranch[AppointmentMetadata]("createAppointment", CreateAppointmentBranchDefect, sdkgo.FailureValidation, "appointment title, time range, or target is invalid")
	}
	targetID, _ := strconv.ParseInt(input.TargetID, 10, 64)
	body, err := json.Marshal(map[string]any{"appointment": map[string]any{"title": input.Title, "from_date": input.FromDate, "end_date": input.EndDate, "targetable_type": input.TargetType, "targetable_id": targetID}})
	if err != nil {
		return createBranch[AppointmentMetadata]("createAppointment", CreateAppointmentBranchDefect, sdkgo.FailureLocalDefect, "appointment request cannot be encoded")
	}
	return invokeLimitedCreate(operation.client, call, "createAppointment", "/appointments", body, CreateAppointmentDefinition.StepDefaults.ExecuteMethodTimeout, createBranches{CreateAppointmentBranchCreated, CreateAppointmentBranchProviderRejected, CreateAppointmentBranchDefect}, decodeAppointment, func(value AppointmentMetadata) string { return strconv.FormatInt(value.ID, 10) }, func(id string) string { return "/appointments/" + id })
}

// Invoke replaces the title and confirms the appointment by read-back.
func (operation UpdateAppointmentOperation) Invoke(call sdkgo.Call, input UpdateAppointmentInput) sdkgo.MutationAttempt[AppointmentMetadata] {
	if !isPositiveContactID(input.AppointmentID) || validateCRMName(input.Title) != nil {
		return createBranch[AppointmentMetadata]("updateAppointment", UpdateAppointmentBranchDefect, sdkgo.FailureValidation, "appointment ID or title is invalid")
	}
	path := "/appointments/" + input.AppointmentID
	body, status, delay, err := operation.client.requestJSON(call, call.Context, http.MethodPut, path, map[string]any{"appointment": map[string]string{"title": input.Title}})
	if attempt := classifyAppointmentUpdate("updateAppointment", status, delay, err); attempt != nil {
		return *attempt
	}
	updated, decodeErr := decodeAppointment(body)
	if decodeErr != nil || strconv.FormatInt(updated.ID, 10) != input.AppointmentID {
		return createRetry[AppointmentMetadata]("updateAppointment", sdkgo.FailureProtocol, "appointment update answer unconfirmed", 0)
	}
	confirmedBody, status, delay, err := operation.client.get(call, path)
	if attempt := classifyAppointmentUpdate("updateAppointment", status, delay, err); attempt != nil {
		return *attempt
	}
	confirmed, decodeErr := decodeAppointment(confirmedBody)
	if decodeErr != nil || confirmed.ID != updated.ID || confirmed.Title != input.Title {
		return createRetry[AppointmentMetadata]("updateAppointment", sdkgo.FailureProtocol, "updated appointment not yet confirmed", 0)
	}
	return sdkgo.NewMutationBranch(UpdateAppointmentBranchUpdated, confirmed, nil, createReceipt(call, input.AppointmentID))
}

// Invoke deletes one appointment by ID.
func (operation DeleteAppointmentOperation) Invoke(call sdkgo.Call, input DeleteAppointmentInput) sdkgo.MutationAttempt[DeletedCRMRecord] {
	if !isPositiveContactID(input.AppointmentID) {
		return deletedNamedBranch("deleteAppointment", DeleteAppointmentBranchDefect, sdkgo.FailureValidation, "appointment ID is invalid")
	}
	id, _ := strconv.ParseInt(input.AppointmentID, 10, 64)
	return invokeNamedDelete(operation.client, call, "deleteAppointment", "/appointments/"+input.AppointmentID, id, DeleteAppointmentBranchDeleted, DeleteAppointmentBranchProviderRejected, DeleteAppointmentBranchDefect)
}

func classifyAppointmentUpdate(operation string, status int, delay time.Duration, err error) *sdkgo.MutationAttempt[AppointmentMetadata] {
	var attempt sdkgo.MutationAttempt[AppointmentMetadata]
	switch {
	case errors.Is(err, errCredential):
		attempt = createBranch[AppointmentMetadata](operation, UpdateAppointmentBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable or invalid")
	case status == http.StatusTooManyRequests:
		attempt = createRetry[AppointmentMetadata](operation, sdkgo.FailureRateLimit, "Freshsales rate limited appointment update", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		attempt = createRetry[AppointmentMetadata](operation, sdkgo.FailureAvailability, "appointment update outcome unconfirmed", delay)
	case status == http.StatusNotFound:
		attempt = createBranch[AppointmentMetadata](operation, UpdateAppointmentBranchNotFound, sdkgo.FailureProviderRejection, "appointment does not exist")
	case status != http.StatusOK:
		attempt = createBranch[AppointmentMetadata](operation, UpdateAppointmentBranchProviderRejected, sdkgo.FailureProviderRejection, "Freshsales rejected HTTP "+strconv.Itoa(status))
	case err != nil:
		attempt = createRetry[AppointmentMetadata](operation, sdkgo.FailureTransport, "appointment response interrupted", 0)
	default:
		return nil
	}
	return &attempt
}

func validAppointmentTimes(fromDate, endDate string) bool {
	from, fromErr := time.Parse(time.RFC3339, fromDate)
	end, endErr := time.Parse(time.RFC3339, endDate)
	return fromErr == nil && endErr == nil && end.After(from)
}

func decodeAppointment(body []byte) (AppointmentMetadata, error) {
	var envelope struct {
		Appointment struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			FromDate string `json:"from_date"`
			EndDate  string `json:"end_date"`
		} `json:"appointment"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return AppointmentMetadata{}, err
	}
	value := AppointmentMetadata{ID: envelope.Appointment.ID, Title: envelope.Appointment.Title, FromDate: envelope.Appointment.FromDate, EndDate: envelope.Appointment.EndDate}
	if value.ID <= 0 || value.Title == "" {
		return AppointmentMetadata{}, errors.New("appointment identity or title missing")
	}
	return value, nil
}

func decodeAppointmentsPage(body []byte) (AppointmentPage, error) {
	var envelope struct {
		Appointments []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			FromDate string `json:"from_date"`
			EndDate  string `json:"end_date"`
		} `json:"appointments"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return AppointmentPage{}, err
	}
	if envelope.Appointments == nil || envelope.Meta.Total < 0 {
		return AppointmentPage{}, errors.New("appointment page missing")
	}
	result := AppointmentPage{Appointments: make([]AppointmentMetadata, 0, len(envelope.Appointments)), Total: envelope.Meta.Total}
	for _, item := range envelope.Appointments {
		if item.ID <= 0 || item.Title == "" {
			return AppointmentPage{}, errors.New("appointment item invalid")
		}
		result.Appointments = append(result.Appointments, AppointmentMetadata{ID: item.ID, Title: item.Title, FromDate: item.FromDate, EndDate: item.EndDate})
	}
	return result, nil
}
