// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pagerduty

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// UpdateIncidentInput sets one incident to a terminal or acknowledged state.
type UpdateIncidentInput struct {
	// ID is the PagerDuty incident identifier.
	ID string `json:"id"`
	// Status is acknowledged or resolved.
	Status string `json:"status"`
	// FromUserEmail is an existing PagerDuty user's email address for the From header.
	FromUserEmail string `json:"fromUserEmail"`
}

// CreateIncidentInput creates an incident on one service.
type CreateIncidentInput struct {
	// ServiceID identifies the PagerDuty service.
	ServiceID string `json:"serviceId"`
	// Title is the incident summary.
	Title string `json:"title"`
	// FromUserEmail is an existing PagerDuty user's email address for the From header.
	FromUserEmail string `json:"fromUserEmail"`
}

// CreateIncidentNoteInput adds text to one incident.
type CreateIncidentNoteInput struct {
	// IncidentID identifies the target incident.
	IncidentID string `json:"incidentId"`
	// Content is the note text.
	Content string `json:"content"`
	// FromUserEmail is an existing PagerDuty user's email address for the From header.
	FromUserEmail string `json:"fromUserEmail"`
}

// UpdateIncidentOperation changes one incident status.
type UpdateIncidentOperation struct{ client *Client }

// Definition returns the generated updateIncident contract.
func (UpdateIncidentOperation) Definition() sdkgo.MutationDefinition { return UpdateIncidentDefinition }

// IdempotencyKey derives a stable key from the Dex call.
func (UpdateIncidentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateIncidentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// CreateIncidentOperation creates one incident with a stable incident key.
type CreateIncidentOperation struct{ client *Client }

// Definition returns the generated createIncident contract.
func (CreateIncidentOperation) Definition() sdkgo.MutationDefinition { return CreateIncidentDefinition }

// IdempotencyKey derives the incident key from the Dex call.
func (CreateIncidentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIncidentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// CreateIncidentNoteOperation adds one note to an incident.
type CreateIncidentNoteOperation struct{ client *Client }

// Definition returns the generated createIncidentNote contract.
func (CreateIncidentNoteOperation) Definition() sdkgo.MutationDefinition {
	return CreateIncidentNoteDefinition
}

// IdempotencyKey derives a stable local call identity; PagerDuty does not deduplicate notes by it.
func (CreateIncidentNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIncidentNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// UpdateIncident binds the status operation to this client.
func (client *Client) UpdateIncident() UpdateIncidentOperation {
	return UpdateIncidentOperation{client}
}

// CreateIncident binds incident creation to this client.
func (client *Client) CreateIncident() CreateIncidentOperation {
	return CreateIncidentOperation{client}
}

// CreateIncidentNote binds note creation to this client.
func (client *Client) CreateIncidentNote() CreateIncidentNoteOperation {
	return CreateIncidentNoteOperation{client}
}

// Invoke replaces the incident status with the requested state.
func (operation UpdateIncidentOperation) Invoke(call sdkgo.Call, input UpdateIncidentInput) sdkgo.MutationAttempt[Incident] {
	if strings.TrimSpace(input.ID) == "" || !validPagerDutyActor(input.FromUserEmail) || (input.Status != "acknowledged" && input.Status != "resolved") {
		return mutationFailure[Incident]("updateIncident", UpdateIncidentBranchDefect, sdkgo.FailureValidation, "incident ID, acknowledged or resolved status, and actor email are required")
	}
	body, err := json.Marshal(map[string]any{"incident": map[string]string{"type": "incident_reference", "status": input.Status}})
	if err != nil {
		return mutationFailure[Incident]("updateIncident", UpdateIncidentBranchDefect, sdkgo.FailureLocalDefect, "incident request could not be encoded")
	}
	return runMutation(operation.client, call, "updateIncident", http.MethodPut, "/incidents/"+url.PathEscape(input.ID), body, "From", input.FromUserEmail, UpdateIncidentBranchUpdated, UpdateIncidentBranchNotFound, UpdateIncidentBranchProviderRejected, UpdateIncidentBranchInvalidResponse, UpdateIncidentBranchDefect, decodeIncidentEnvelope)
}

// Invoke creates an incident using this call's stable incident key.
func (operation CreateIncidentOperation) Invoke(call sdkgo.Call, input CreateIncidentInput) sdkgo.MutationAttempt[Incident] {
	if strings.TrimSpace(input.ServiceID) == "" || strings.TrimSpace(input.Title) == "" || !validPagerDutyActor(input.FromUserEmail) {
		return mutationFailure[Incident]("createIncident", CreateIncidentBranchDefect, sdkgo.FailureValidation, "service ID, title, and actor email are required")
	}
	body, err := json.Marshal(map[string]any{"incident": map[string]any{"type": "incident", "title": input.Title, "service": map[string]string{"id": input.ServiceID, "type": "service_reference"}, "incident_key": string(call.IdempotencyKey)}})
	if err != nil {
		return mutationFailure[Incident]("createIncident", CreateIncidentBranchDefect, sdkgo.FailureLocalDefect, "incident request could not be encoded")
	}
	return runLimitedCreate(operation.client, call, "createIncident", "/incidents", body, input.FromUserEmail, CreateIncidentBranchCreated, "", CreateIncidentBranchProviderRejected, CreateIncidentBranchDefect, decodeIncidentEnvelope, func() (Incident, bool) {
		return operation.client.readBackIncident(call, string(call.IdempotencyKey), input.ServiceID)
	})
}

// Invoke adds an incident note with bounded possible sends.
func (operation CreateIncidentNoteOperation) Invoke(call sdkgo.Call, input CreateIncidentNoteInput) sdkgo.MutationAttempt[IncidentNote] {
	if strings.TrimSpace(input.IncidentID) == "" || strings.TrimSpace(input.Content) == "" || !validPagerDutyActor(input.FromUserEmail) {
		return mutationFailure[IncidentNote]("createIncidentNote", CreateIncidentNoteBranchDefect, sdkgo.FailureValidation, "incident ID, note content, and actor email are required")
	}
	body, err := json.Marshal(map[string]any{"note": map[string]string{"content": input.Content}})
	if err != nil {
		return mutationFailure[IncidentNote]("createIncidentNote", CreateIncidentNoteBranchDefect, sdkgo.FailureLocalDefect, "note request could not be encoded")
	}
	return runLimitedCreate(operation.client, call, "createIncidentNote", "/incidents/"+url.PathEscape(input.IncidentID)+"/notes", body, input.FromUserEmail, CreateIncidentNoteBranchCreated, CreateIncidentNoteBranchNotFound, CreateIncidentNoteBranchProviderRejected, CreateIncidentNoteBranchDefect, decodeNoteEnvelope, nil)
}

func validPagerDutyActor(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && address.Name == "" && providerhttp.IsHeaderSafeCredential(email)
}

func decodeIncidentEnvelope(body []byte) (Incident, error) {
	var envelope struct {
		Incident Incident `json:"incident"`
	}
	err := json.Unmarshal(body, &envelope)
	if err == nil && envelope.Incident.ID == "" {
		err = fmt.Errorf("incident missing")
	}
	return envelope.Incident, err
}

func decodeNoteEnvelope(body []byte) (IncidentNote, error) {
	var envelope struct {
		Note IncidentNote `json:"note"`
	}
	err := json.Unmarshal(body, &envelope)
	if err == nil && envelope.Note.ID == "" {
		err = fmt.Errorf("note missing")
	}
	return envelope.Note, err
}

func (client *Client) readBackIncident(call sdkgo.Call, incidentKey, serviceID string) (Incident, bool) {
	query := url.Values{"incident_key": {incidentKey}, "service_ids[]": {serviceID}}
	request, err := client.newRequest(call, http.MethodGet, "/incidents?"+query.Encode(), nil)
	if err != nil {
		return Incident{}, false
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return Incident{}, false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Incident{}, false
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return Incident{}, false
	}
	var page IncidentPage
	if json.Unmarshal(content, &page) != nil {
		return Incident{}, false
	}
	for _, incident := range page.Incidents {
		if incident.ID != "" {
			return incident, true
		}
	}
	return Incident{}, false
}

func runLimitedCreate[T any](client *Client, call sdkgo.Call, operation, path string, body []byte, actorEmail string, created, notFound, rejected, defect sdkgo.BranchID, decode func([]byte) (T, error), readBack func() (T, bool)) sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(call, 30*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationFailure[T](operation, defect, sdkgo.FailureLocalDefect, "dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return uncertainCreate[T](operation)
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		if readBack != nil {
			if value, found := readBack(); found {
				return sdkgo.NewMutationBranch(created, value, nil, sdkgo.Receipt{})
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return uncertainCreate[T](operation)
		}
	}
	request, err := client.newRequest(call, http.MethodPost, path, body)
	if err != nil {
		if decision.MaximumPriorSends > 0 {
			return uncertainCreate[T](operation)
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return mutationFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request.Header.Set("From", actorEmail)
	var response *http.Response
	err = fence.Dispatch(func(ctx context.Context) error {
		var requestErr error
		response, requestErr = client.httpClient.Do(request.WithContext(ctx))
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "request was not sent before the dispatch checkpoint"), 0)
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	if err != nil || response == nil {
		if !canResend {
			return uncertainCreate[T](operation)
		}
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider request outcome is unknown"), 0)
	}
	// Response close cannot change the completed provider decision.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[T](newFailure(operation, kind, "provider asked to retry the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		if !canResend {
			return uncertainCreate[T](operation)
		}
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider write outcome is unknown"), 0)
	}
	if response.StatusCode == http.StatusNotFound && notFound != "" {
		if decision.MaximumPriorSends > 0 {
			return uncertainCreate[T](operation)
		}
		return mutationFailure[T](operation, notFound, sdkgo.FailureNotFound, "incident was not found")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if decision.MaximumPriorSends > 0 {
			return uncertainCreate[T](operation)
		}
		return mutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the creation")
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return uncertainCreate[T](operation)
	}
	value, err := decode(content)
	if err != nil {
		return uncertainCreate[T](operation)
	}
	return sdkgo.NewMutationBranch(created, value, nil, sdkgo.Receipt{})
}

func uncertainCreate[T any](operation string) sdkgo.MutationAttempt[T] {
	var zero T
	return sdkgo.NewMutationUncertain(zero, newFailure(operation, sdkgo.FailureTransport, "provider write may have succeeded"), sdkgo.Receipt{})
}
