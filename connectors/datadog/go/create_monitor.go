// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadog

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateMonitorInput creates one metric alert monitor.
type CreateMonitorInput struct {
	// Name is the visible monitor name; open Monitors > New Monitor in Datadog to choose it.
	Name string `json:"name"`
	// Query is a Datadog metric alert query, such as an average metric threshold expression.
	Query string `json:"query"`
	// Message is the alert notification text; omit recipient mentions to avoid notifying users.
	Message string `json:"message"`
}

// CreateMonitorOperation creates a monitor with bounded repeat sends.
type CreateMonitorOperation struct{ client *Client }

// CreateMonitor binds the mutation to this client.
func (client *Client) CreateMonitor() CreateMonitorOperation { return CreateMonitorOperation{client} }

// Definition returns the generated mutation contract.
func (CreateMonitorOperation) Definition() sdkgo.MutationDefinition { return CreateMonitorDefinition }

// IdempotencyKey correlates the call; Datadog does not accept a creation deduplication key.
func (CreateMonitorOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateMonitorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one monitor after a durable send checkpoint.
func (operation CreateMonitorOperation) Invoke(call sdkgo.Call, input CreateMonitorInput) sdkgo.MutationAttempt[Monitor] {
	if !validCreateMonitorInput(input) {
		return monitorMutationBranch[Monitor](CreateMonitorBranchDefect, "createMonitor", sdkgo.FailureValidation, "name, metric query, and message must be valid nonblank UTF-8")
	}
	fence, err := singledispatch.NewFence(call, CreateMonitorDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return monitorMutationBranch[Monitor](CreateMonitorBranchDefect, "createMonitor", sdkgo.FailureLocalDefect, "monitor creation checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return operation.uncertainCreate(call, "monitor creation checkpoint is unreadable")
	}
	if decision.Marker.ProviderID != "" {
		return operation.readCreatedMonitor(call, input, decision.Marker.ProviderID)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return operation.uncertainCreate(call, "earlier monitor creation may have applied without a confirmed ID")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) || !providerhttp.IsHeaderSafeCredential(credentials.ApplicationKey.Reveal()) {
		if decision.Kind == singledispatch.SendAllowed {
			_ = fence.RecordNotSent()
		}
		return monitorMutationBranch[Monitor](CreateMonitorBranchDefect, "createMonitor", sdkgo.FailureAuthentication, "Datadog credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Query   string `json:"query"`
		Message string `json:"message"`
	}{Name: input.Name, Type: "metric alert", Query: input.Query, Message: input.Message})
	if err != nil || len(body) > 1<<20 {
		return monitorMutationBranch[Monitor](CreateMonitorBranchDefect, "createMonitor", sdkgo.FailureValidation, "monitor request exceeds the size limit")
	}
	send := &createMonitorSend{client: operation.client, call: call, body: body}
	if err := fence.Dispatch(send.execute); err != nil {
		return monitorMutationRetry[Monitor]("createMonitor", sdkgo.FailureAvailability, "monitor creation was not dispatched after checkpoint", 0)
	}
	if send.localFailure != nil {
		_ = fence.RecordCanResend()
		return monitorMutationRetry[Monitor]("createMonitor", send.localFailure.Kind, "Datadog credentials became unavailable before dispatch", 0)
	}
	if send.err != nil {
		return operation.retryOrUncertain(call, decision, 0, "Datadog monitor creation outcome is unknown")
	}
	response := send.response
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		_ = fence.RecordCanResend()
		retry, after := retryFailure(response, "createMonitor")
		return sdkgo.NewMutationRetry[Monitor](retry, after)
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		_, after := retryFailure(response, "createMonitor")
		return operation.retryOrUncertain(call, decision, after, "Datadog monitor creation outcome is unknown")
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		if decision.Kind != singledispatch.SendAllowed {
			return operation.retryOrUncertain(call, decision, 0, "earlier monitor creation may have applied before a later refusal")
		}
		return monitorMutationBranch[Monitor](CreateMonitorBranchProviderRejected, "createMonitor", statusFailure(response, "createMonitor").Kind, "Datadog rejected monitor creation with HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return operation.retryOrUncertain(call, decision, 0, "Datadog accepted monitor creation without a usable response")
	}
	var monitor Monitor
	if json.Unmarshal(responseBody, &monitor) != nil {
		return operation.retryOrUncertain(call, decision, 0, "Datadog accepted monitor creation without a confirmed monitor ID")
	}
	if monitor.ID > 0 {
		_ = fence.RecordProviderID(strconv.FormatInt(monitor.ID, 10))
	}
	if monitor.ID <= 0 {
		return operation.retryOrUncertain(call, decision, 0, "Datadog accepted monitor creation without a confirmed monitor ID")
	}
	monitorID := strconv.FormatInt(monitor.ID, 10)
	if monitor.Name != input.Name || monitor.Type != "metric alert" {
		return operation.readCreatedMonitor(call, input, monitorID)
	}
	return sdkgo.NewMutationBranch(CreateMonitorBranchCreated, monitor, nil, monitorCreateReceipt(call, monitorID))
}

func validCreateMonitorInput(input CreateMonitorInput) bool {
	for _, value := range []string{input.Name, input.Query, input.Message} {
		if strings.TrimSpace(value) == "" || len(value) > 100000 || !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func (operation CreateMonitorOperation) readCreatedMonitor(call sdkgo.Call, input CreateMonitorInput, monitorID string) sdkgo.MutationAttempt[Monitor] {
	response, localFailure, err := operation.client.send(call, "createMonitor", requestSpec{method: http.MethodGet, path: "/api/v1/monitor/" + monitorID})
	if localFailure != nil || err != nil {
		return monitorMutationRetry[Monitor]("createMonitor", sdkgo.FailureAvailability, "created monitor read-back is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, "createMonitor")
		return sdkgo.NewMutationRetry[Monitor](retry, after)
	}
	if response.StatusCode != http.StatusOK {
		return operation.uncertainCreate(call, "created monitor could not be read back")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return monitorMutationRetry[Monitor]("createMonitor", sdkgo.FailureProtocol, "created monitor read-back was interrupted", 0)
	}
	var monitor Monitor
	if json.Unmarshal(body, &monitor) != nil || strconv.FormatInt(monitor.ID, 10) != monitorID || monitor.Name != input.Name || monitor.Type != "metric alert" {
		return operation.uncertainCreate(call, "created monitor identity differs from the request")
	}
	return sdkgo.NewMutationBranch(CreateMonitorBranchCreated, monitor, nil, monitorCreateReceipt(call, monitorID))
}

func (operation CreateMonitorOperation) retryOrUncertain(call sdkgo.Call, decision singledispatch.Decision, after time.Duration, message string) sdkgo.MutationAttempt[Monitor] {
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return operation.uncertainCreate(call, message)
	}
	return monitorMutationRetry[Monitor]("createMonitor", sdkgo.FailureAvailability, message, after)
}

func (CreateMonitorOperation) uncertainCreate(call sdkgo.Call, message string) sdkgo.MutationAttempt[Monitor] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "datadog", Operation: "createMonitor", Message: message}
	return sdkgo.NewMutationUncertain(Monitor{}, failure, monitorCreateReceipt(call, ""))
}

func monitorCreateReceipt(call sdkgo.Call, monitorID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "datadog", ProviderObjectID: monitorID, ObservedAt: time.Now().UTC()}
}

type createMonitorSend struct {
	client       *Client
	call         sdkgo.Call
	body         []byte
	response     *http.Response
	localFailure *sdkgo.Failure
	err          error
}

func (send *createMonitorSend) execute(ctx context.Context) error {
	send.response, send.localFailure, send.err = send.client.sendWithContext(ctx, send.call, "createMonitor", requestSpec{method: http.MethodPost, path: "/api/v1/monitor", contentType: "application/json", body: send.body})
	return nil
}
