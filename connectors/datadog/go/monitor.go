// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetMonitorInput selects a monitor by its numeric ID.
type GetMonitorInput struct {
	// MonitorID is the ID shown on the Datadog Monitors page.
	MonitorID int64 `json:"monitorId"`
}

// UpdateMonitorInput replaces selected monitor fields with absolute values.
type UpdateMonitorInput struct {
	// MonitorID identifies the monitor to edit.
	MonitorID int64 `json:"monitorId"`
	// Name replaces the monitor name when non-nil; it cannot be blank.
	Name *string `json:"name,omitempty"`
	// Message replaces the notification message when non-nil; an empty string clears it.
	Message *string `json:"message,omitempty"`
	// Query replaces the monitor query when non-nil; it cannot be blank.
	Query *string `json:"query,omitempty"`
}

// DeleteMonitorInput selects a monitor to delete without force.
type DeleteMonitorInput struct {
	// MonitorID is the monitor ID; dependent resources can cause provider rejection.
	MonitorID int64 `json:"monitorId"`
}

// DeletedMonitor identifies a monitor Datadog confirmed was deleted.
type DeletedMonitor struct {
	// MonitorID is the deleted monitor's ID.
	MonitorID int64 `json:"monitorId"`
}

// GetMonitorOperation reads one monitor.
type GetMonitorOperation struct{ client *Client }

// UpdateMonitorOperation sets selected monitor fields to absolute values.
type UpdateMonitorOperation struct{ client *Client }

// DeleteMonitorOperation deletes one monitor.
type DeleteMonitorOperation struct{ client *Client }

// GetMonitor binds the query to this client.
func (client *Client) GetMonitor() GetMonitorOperation { return GetMonitorOperation{client} }

// UpdateMonitor binds the mutation to this client.
func (client *Client) UpdateMonitor() UpdateMonitorOperation { return UpdateMonitorOperation{client} }

// DeleteMonitor binds the mutation to this client.
func (client *Client) DeleteMonitor() DeleteMonitorOperation { return DeleteMonitorOperation{client} }

// Definition returns the generated getMonitor query definition.
func (GetMonitorOperation) Definition() sdkgo.QueryDefinition { return GetMonitorDefinition }

// Definition returns the generated updateMonitor mutation definition.
func (UpdateMonitorOperation) Definition() sdkgo.MutationDefinition { return UpdateMonitorDefinition }

// Definition returns the generated deleteMonitor mutation definition.
func (DeleteMonitorOperation) Definition() sdkgo.MutationDefinition { return DeleteMonitorDefinition }

// IdempotencyKey derives a stable call identity; repeated absolute updates converge.
func (UpdateMonitorOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateMonitorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call identity; repeated deletion converges.
func (DeleteMonitorOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteMonitorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke gets the monitor without exposing its query, message, or notification recipients.
func (operation GetMonitorOperation) Invoke(call sdkgo.Call, input GetMonitorInput) sdkgo.QueryAttempt[Monitor] {
	if input.MonitorID <= 0 {
		return sdkgo.NewQueryBranch(GetMonitorBranchDefect, Monitor{}, failure(sdkgo.FailureValidation, "getMonitor", "monitorId must be positive"), sdkgo.Receipt{})
	}
	path := "/api/v1/monitor/" + strconv.FormatInt(input.MonitorID, 10)
	return executeQuery(operation.client, call, "getMonitor", requestSpec{method: http.MethodGet, path: path},
		GetMonitorBranchFound, GetMonitorBranchNotFound, GetMonitorBranchProviderRejected,
		GetMonitorBranchInvalidResponse, GetMonitorBranchDefect, decodeMonitorForID(input.MonitorID))
}

// Invoke updates absolute fields, so a repeated request converges on the same values.
func (operation UpdateMonitorOperation) Invoke(call sdkgo.Call, input UpdateMonitorInput) sdkgo.MutationAttempt[Monitor] {
	if input.MonitorID <= 0 || !validMonitorUpdate(input) {
		return monitorMutationBranch[Monitor](UpdateMonitorBranchDefect, "updateMonitor", sdkgo.FailureValidation, "a positive monitorId and at least one valid field are required")
	}
	body, err := json.Marshal(struct {
		Name    *string `json:"name,omitempty"`
		Message *string `json:"message,omitempty"`
		Query   *string `json:"query,omitempty"`
	}{Name: input.Name, Message: input.Message, Query: input.Query})
	if err != nil || len(body) > 1<<20 {
		return monitorMutationBranch[Monitor](UpdateMonitorBranchDefect, "updateMonitor", sdkgo.FailureValidation, "monitor update exceeds the 1 MiB request limit")
	}
	path := "/api/v1/monitor/" + strconv.FormatInt(input.MonitorID, 10)
	return executeMonitorMutation(operation.client, call, "updateMonitor", requestSpec{method: http.MethodPut, path: path, contentType: "application/json", body: body},
		UpdateMonitorBranchUpdated, UpdateMonitorBranchNotFound, UpdateMonitorBranchProviderRejected,
		UpdateMonitorBranchInvalidResponse, UpdateMonitorBranchDefect, decodeMonitorForID(input.MonitorID))
}

// Invoke deletes a monitor; a repeated request may select notFound after the first deletion.
func (operation DeleteMonitorOperation) Invoke(call sdkgo.Call, input DeleteMonitorInput) sdkgo.MutationAttempt[DeletedMonitor] {
	if input.MonitorID <= 0 {
		return monitorMutationBranch[DeletedMonitor](DeleteMonitorBranchDefect, "deleteMonitor", sdkgo.FailureValidation, "monitorId must be positive")
	}
	path := "/api/v1/monitor/" + strconv.FormatInt(input.MonitorID, 10)
	return executeMonitorMutation(operation.client, call, "deleteMonitor", requestSpec{method: http.MethodDelete, path: path},
		DeleteMonitorBranchDeleted, DeleteMonitorBranchNotFound, DeleteMonitorBranchProviderRejected,
		DeleteMonitorBranchInvalidResponse, DeleteMonitorBranchDefect, decodeDeletedMonitorForID(input.MonitorID))
}

func validMonitorUpdate(input UpdateMonitorInput) bool {
	if input.Name == nil && input.Message == nil && input.Query == nil {
		return false
	}
	for _, value := range []*string{input.Name, input.Message, input.Query} {
		if value != nil && (!utf8.ValidString(*value) || len(*value) > 100000) {
			return false
		}
	}
	return (input.Name == nil || strings.TrimSpace(*input.Name) != "") && (input.Query == nil || strings.TrimSpace(*input.Query) != "")
}

func decodeMonitorForID(monitorID int64) func([]byte) (Monitor, error) {
	return func(body []byte) (Monitor, error) {
		var monitor Monitor
		if err := json.Unmarshal(body, &monitor); err != nil {
			return Monitor{}, err
		}
		if monitor.ID != monitorID || strings.TrimSpace(monitor.Name) == "" || strings.TrimSpace(monitor.Type) == "" {
			return Monitor{}, errors.New("monitor identity or name is missing")
		}
		return monitor, nil
	}
}

func decodeDeletedMonitorForID(monitorID int64) func([]byte) (DeletedMonitor, error) {
	return func(body []byte) (DeletedMonitor, error) {
		var wire struct {
			ID int64 `json:"deleted_monitor_id"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			return DeletedMonitor{}, err
		}
		if wire.ID != monitorID {
			return DeletedMonitor{}, errors.New("deleted monitor ID does not match request")
		}
		return DeletedMonitor{MonitorID: monitorID}, nil
	}
}

func executeMonitorMutation[T any](client *Client, call sdkgo.Call, operation string, spec requestSpec, happy, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	response, localFailure, err := client.send(call, operation, spec)
	if localFailure != nil {
		return sdkgo.NewMutationBranch(defect, *new(T), localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return monitorMutationRetry[T](operation, sdkgo.FailureTransport, "Datadog request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, operation)
		return sdkgo.NewMutationRetry[T](retry, after)
	}
	if response.StatusCode == http.StatusNotFound {
		providerFailure := statusFailure(response, operation)
		return sdkgo.NewMutationBranch(notFound, *new(T), &providerFailure, sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusOK {
		providerFailure := statusFailure(response, operation)
		return sdkgo.NewMutationBranch(rejected, *new(T), &providerFailure, sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return monitorMutationBranch[T](invalid, operation, sdkgo.FailureResponseTooLarge, "Datadog response exceeded the configured limit")
	}
	if err != nil {
		return monitorMutationRetry[T](operation, sdkgo.FailureTransport, "Datadog response was interrupted", 0)
	}
	value, err := decode(body)
	if err != nil {
		return monitorMutationBranch[T](invalid, operation, sdkgo.FailureProtocol, "Datadog returned an invalid monitor response")
	}
	return sdkgo.NewMutationBranch(happy, value, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "datadog", ObservedAt: time.Now().UTC()})
}

func monitorMutationBranch[T any](branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationBranch(branch, *new(T), failure(kind, operation, message), sdkgo.Receipt{})
}

func monitorMutationRetry[T any](operation string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "datadog", Operation: operation, Message: message}, after)
}
