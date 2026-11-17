// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onespansign

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// PackagesPage contains bounded transaction identities and the matching count.
type PackagesPage struct {
	// Packages omit signer identities and document contents.
	Packages []PackageStatus `json:"packages"`
	// Count is the provider's total count for the status query.
	Count int `json:"count"`
}

// ListPackagesInput selects an inclusive package page and status.
type ListPackagesInput struct {
	// From is the one-based first package position.
	From int `json:"from"`
	// To is the inclusive last position, at most one hundred items after From.
	To int `json:"to"`
	// Status is DRAFT, SENT, COMPLETED, DECLINED, EXPIRED, or ARCHIVED.
	Status string `json:"status"`
}

// CreateDraftPackageInput names a new draft transaction.
type CreateDraftPackageInput struct {
	// Name is the non-empty transaction title.
	Name string `json:"name"`
}

// CreatedPackage identifies the new draft transaction.
type CreatedPackage struct {
	// ID is the returned package ID.
	ID string `json:"id"`
}

// UpdatePackageNameInput selects a draft and its new title.
type UpdatePackageNameInput struct {
	// PackageID is the package to update.
	PackageID string `json:"packageId"`
	// Name is the new non-empty title.
	Name string `json:"name"`
}

// UpdatedPackage identifies a package after a title update.
type UpdatedPackage struct {
	// ID is the package ID.
	ID string `json:"id"`
	// Name is the requested title.
	Name string `json:"name"`
}

// SendPackageInput selects a prepared draft for delivery.
type SendPackageInput struct {
	// PackageID is the transaction to send.
	PackageID string `json:"packageId"`
}

// SentPackage identifies the transaction sent for signing.
type SentPackage struct {
	// ID is the package ID.
	ID string `json:"id"`
	// Status is SENT.
	Status string `json:"status"`
}

// TrashPackageInput selects a transaction to move to Trash.
type TrashPackageInput struct {
	// PackageID is the transaction ID.
	PackageID string `json:"packageId"`
}

// TrashedPackage identifies the transaction moved to Trash.
type TrashedPackage struct {
	// ID is the package ID.
	ID string `json:"id"`
}

// DeletePackageInput selects a transaction for permanent deletion.
type DeletePackageInput struct {
	// PackageID is the transaction ID.
	PackageID string `json:"packageId"`
}

// DeletedPackage identifies the permanently deleted transaction.
type DeletedPackage struct {
	// ID is the package ID.
	ID string `json:"id"`
}

func (client *Client) requestPackage(call sdkgo.Call, requestContext context.Context, method, path string, body []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Basic "+credentials.APIKey.Reveal())
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, readError := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: readError}
}

func packageOperationFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "onespan-sign", Operation: operation, Message: message}
}

func packageQueryBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := packageOperationFailure(operation, kind, message)
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func packageMutationBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := packageOperationFailure(operation, kind, message)
	var zero T
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func validPackageName(name string) bool {
	return len(name) > 0 && len(name) <= 256 && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "\r\n\x00")
}

func validPackageStatus(status string) bool {
	switch status {
	case "DRAFT", "SENT", "COMPLETED", "DECLINED", "EXPIRED", "ARCHIVED":
		return true
	default:
		return false
	}
}

func decodePackagesPage(body []byte, maximum int) (PackagesPage, error) {
	var wire struct {
		Results []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"results"`
		Count int `json:"count"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Results == nil || wire.Count < 0 || len(wire.Results) > maximum {
		return PackagesPage{}, errors.New("package page is invalid")
	}
	packages := make([]PackageStatus, 0, len(wire.Results))
	for _, item := range wire.Results {
		if !validPackageID(item.ID) || !validPackageName(item.Name) || !validPackageStatus(item.Status) {
			return PackagesPage{}, errors.New("package identity is invalid")
		}
		packages = append(packages, PackageStatus{ID: item.ID, Name: item.Name, Status: item.Status})
	}
	return PackagesPage{Packages: packages, Count: wire.Count}, nil
}

// ListPackagesOperation lists transaction identities by status.
type ListPackagesOperation struct{ client *Client }

// ListPackages binds the package list Query.
func (client *Client) ListPackages() ListPackagesOperation { return ListPackagesOperation{client} }

// Definition returns the generated list contract.
func (ListPackagesOperation) Definition() sdkgo.QueryDefinition { return ListPackagesDefinition }

// Invoke reads one inclusive page without signer or document details.
func (operation ListPackagesOperation) Invoke(call sdkgo.Call, input ListPackagesInput) sdkgo.QueryAttempt[PackagesPage] {
	if input.From < 1 || input.To < input.From || input.To-input.From >= 100 || !validPackageStatus(input.Status) {
		return packageQueryBranch[PackagesPage]("listPackages", ListPackagesBranchDefect, sdkgo.FailureValidation, "page or status is invalid")
	}
	query := url.Values{"from": {strconv.Itoa(input.From)}, "to": {strconv.Itoa(input.To)}, "query": {input.Status}}
	response := operation.client.requestPackage(call, call.Context, http.MethodGet, "/packages?"+query.Encode(), nil)
	if errors.Is(response.err, errCredential) {
		return packageQueryBranch[PackagesPage]("listPackages", ListPackagesBranchDefect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PackagesPage](packageOperationFailure("listPackages", sdkgo.FailureTransport, "package list request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PackagesPage](packageOperationFailure("listPackages", sdkgo.FailureRateLimit, "provider rate limited the list"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PackagesPage](packageOperationFailure("listPackages", sdkgo.FailureAvailability, "provider is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return packageQueryBranch[PackagesPage]("listPackages", ListPackagesBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected the list")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return packageQueryBranch[PackagesPage]("listPackages", ListPackagesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "package list exceeded the response limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PackagesPage](packageOperationFailure("listPackages", sdkgo.FailureTransport, "package list response was interrupted"), 0)
	}
	page, err := decodePackagesPage(response.body, input.To-input.From+1)
	if err != nil {
		return packageQueryBranch[PackagesPage]("listPackages", ListPackagesBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned an invalid package page")
	}
	return sdkgo.NewQueryBranch(ListPackagesBranchListed, page, nil, sdkgo.Receipt{})
}

// CreateDraftPackageOperation creates a document-free draft.
type CreateDraftPackageOperation struct{ client *Client }

// CreateDraftPackage binds draft package creation.
func (client *Client) CreateDraftPackage() CreateDraftPackageOperation {
	return CreateDraftPackageOperation{client}
}

// Definition returns the generated creation contract.
func (CreateDraftPackageOperation) Definition() sdkgo.MutationDefinition {
	return CreateDraftPackageDefinition
}

// IdempotencyKey identifies this call; OneSpan Sign accepts no idempotency header.
func (CreateDraftPackageOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDraftPackageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a draft with a three-send cap and reports ambiguous outcomes.
func (operation CreateDraftPackageOperation) Invoke(call sdkgo.Call, input CreateDraftPackageInput) sdkgo.MutationAttempt[CreatedPackage] {
	if !validPackageName(input.Name) {
		return packageMutationBranch[CreatedPackage]("createDraftPackage", CreateDraftPackageBranchDefect, sdkgo.FailureValidation, "package name is invalid")
	}
	body, _ := json.Marshal(map[string]any{"name": input.Name, "status": "DRAFT", "type": "PACKAGE"})
	fence, err := singledispatch.NewFence(call, CreateDraftPackageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return packageMutationBranch[CreatedPackage]("createDraftPackage", CreateDraftPackageBranchDefect, sdkgo.FailureLocalDefect, "dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CreatedPackage{}, packageOperationFailure("createDraftPackage", sdkgo.FailureTransport, "draft creation may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestPackage(call, requestContext, http.MethodPost, "/packages", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CreatedPackage](packageOperationFailure("createDraftPackage", sdkgo.FailureAvailability, "draft was not dispatched"), 0)
	}
	if errors.Is(err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedPackage{}, packageOperationFailure("createDraftPackage", sdkgo.FailureAuthentication, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[CreatedPackage](packageOperationFailure("createDraftPackage", sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return packageMutationBranch[CreatedPackage]("createDraftPackage", CreateDraftPackageBranchDefect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CreatedPackage](packageOperationFailure("createDraftPackage", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CreatedPackage](packageOperationFailure("createDraftPackage", kind, "provider temporarily rejected the draft"), response.retryAfter)
	}
	if err != nil || response.status >= 500 {
		failure := packageOperationFailure("createDraftPackage", sdkgo.FailureAvailability, "draft create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[CreatedPackage](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(CreatedPackage{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedPackage{}, packageOperationFailure("createDraftPackage", sdkgo.FailureProviderRejection, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		return packageMutationBranch[CreatedPackage]("createDraftPackage", CreateDraftPackageBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected draft creation")
	}
	var created CreatedPackage
	if json.Unmarshal(response.body, &created) != nil || !validPackageID(created.ID) {
		return sdkgo.NewMutationUncertain(CreatedPackage{}, packageOperationFailure("createDraftPackage", sdkgo.FailureProtocol, "provider accepted draft without a valid ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateDraftPackageBranchCreated, created, nil, sdkgo.Receipt{})
}

func invokePackageWrite[T any](call sdkgo.Call, client *Client, operation, method, path string, body []byte, success, notFound, rejected, invalid, defect sdkgo.BranchID, result T) sdkgo.MutationAttempt[T] {
	response := client.requestPackage(call, call.Context, method, path, body)
	if errors.Is(response.err, errCredential) {
		return packageMutationBranch[T](operation, defect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[T](packageOperationFailure(operation, sdkgo.FailureTransport, "provider request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](packageOperationFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[T](packageOperationFailure(operation, sdkgo.FailureAvailability, "provider is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return packageMutationBranch[T](operation, notFound, sdkgo.FailureNotFound, "package was not found")
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return packageMutationBranch[T](operation, rejected, sdkgo.FailureProviderRejection, fmt.Sprintf("provider rejected HTTP %d", response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return packageMutationBranch[T](operation, invalid, sdkgo.FailureResponseTooLarge, "response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[T](packageOperationFailure(operation, sdkgo.FailureTransport, "provider response was interrupted"), 0)
	}
	return sdkgo.NewMutationBranch(success, result, nil, sdkgo.Receipt{})
}

// UpdatePackageNameOperation changes a draft's title.
type UpdatePackageNameOperation struct{ client *Client }

// UpdatePackageName binds the title update.
func (client *Client) UpdatePackageName() UpdatePackageNameOperation {
	return UpdatePackageNameOperation{client}
}

// Definition returns the generated title update contract.
func (UpdatePackageNameOperation) Definition() sdkgo.MutationDefinition {
	return UpdatePackageNameDefinition
}

// IdempotencyKey identifies an absolute title assignment across retries.
func (UpdatePackageNameOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePackageNameInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets a draft's name to the supplied title.
func (operation UpdatePackageNameOperation) Invoke(call sdkgo.Call, input UpdatePackageNameInput) sdkgo.MutationAttempt[UpdatedPackage] {
	if !validPackageID(input.PackageID) || !validPackageName(input.Name) {
		return packageMutationBranch[UpdatedPackage]("updatePackageName", UpdatePackageNameBranchDefect, sdkgo.FailureValidation, "package ID or name is invalid")
	}
	body, _ := json.Marshal(map[string]any{"name": input.Name})
	return invokePackageWrite(call, operation.client, "updatePackageName", http.MethodPut, "/packages/"+url.PathEscape(input.PackageID), body, UpdatePackageNameBranchUpdated, UpdatePackageNameBranchNotFound, UpdatePackageNameBranchProviderRejected, UpdatePackageNameBranchInvalidResponse, UpdatePackageNameBranchDefect, UpdatedPackage{ID: input.PackageID, Name: input.Name})
}

// SendPackageOperation sends a prepared transaction with limited retry sends.
type SendPackageOperation struct{ client *Client }

// SendPackage binds package delivery.
func (client *Client) SendPackage() SendPackageOperation { return SendPackageOperation{client} }

// Definition returns the generated send contract.
func (SendPackageOperation) Definition() sdkgo.MutationDefinition { return SendPackageDefinition }

// IdempotencyKey identifies the delivery call; OneSpan Sign accepts no key header.
func (SendPackageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendPackageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a prepared package and reads its status after an ambiguous send.
func (operation SendPackageOperation) Invoke(call sdkgo.Call, input SendPackageInput) sdkgo.MutationAttempt[SentPackage] {
	if !validPackageID(input.PackageID) {
		return packageMutationBranch[SentPackage]("sendPackage", SendPackageBranchDefect, sdkgo.FailureValidation, "package ID is invalid")
	}
	fence, err := singledispatch.NewFence(call, SendPackageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return packageMutationBranch[SentPackage]("sendPackage", SendPackageBranchDefect, sdkgo.FailureLocalDefect, "dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.MaximumPriorSends > 0 || decision.Kind != singledispatch.SendAllowed {
		response := operation.client.requestPackage(call, call.Context, http.MethodGet, "/packages/"+url.PathEscape(input.PackageID), nil)
		if response.status == http.StatusOK && response.err == nil {
			status, parseError := decodePackage(response.body, input.PackageID)
			if parseError == nil && (status.Status == "SENT" || status.Status == "COMPLETED" || status.Status == "ARCHIVED") {
				return sdkgo.NewMutationBranch(SendPackageBranchSent, SentPackage{ID: input.PackageID, Status: status.Status}, nil, sdkgo.Receipt{})
			}
		} else if response.err != nil || response.status >= 500 {
			return sdkgo.NewMutationRetry[SentPackage](packageOperationFailure("sendPackage", sdkgo.FailureAvailability, "send read-back is unavailable"), response.retryAfter)
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(SentPackage{}, packageOperationFailure("sendPackage", sdkgo.FailureTransport, "delivery may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestPackage(call, requestContext, http.MethodPost, "/packages/"+url.PathEscape(input.PackageID)+"/status", []byte(`{"status":"SENT"}`))
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SentPackage](packageOperationFailure("sendPackage", sdkgo.FailureAvailability, "send was not dispatched"), 0)
	}
	if errors.Is(err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SentPackage{}, packageOperationFailure("sendPackage", sdkgo.FailureAuthentication, "earlier send may have succeeded"), sdkgo.Receipt{})
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[SentPackage](packageOperationFailure("sendPackage", sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return packageMutationBranch[SentPackage]("sendPackage", SendPackageBranchDefect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[SentPackage](packageOperationFailure("sendPackage", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SentPackage](packageOperationFailure("sendPackage", kind, "provider temporarily rejected the send"), response.retryAfter)
	}
	if err != nil || response.status >= 500 {
		failure := packageOperationFailure("sendPackage", sdkgo.FailureAvailability, "delivery outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[SentPackage](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(SentPackage{}, failure, sdkgo.Receipt{})
	}
	if response.status == http.StatusNotFound {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SentPackage{}, packageOperationFailure("sendPackage", sdkgo.FailureNotFound, "earlier delivery may have succeeded"), sdkgo.Receipt{})
		}
		return packageMutationBranch[SentPackage]("sendPackage", SendPackageBranchNotFound, sdkgo.FailureNotFound, "package was not found")
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(SentPackage{}, packageOperationFailure("sendPackage", sdkgo.FailureProviderRejection, "earlier delivery may have succeeded"), sdkgo.Receipt{})
		}
		return packageMutationBranch[SentPackage]("sendPackage", SendPackageBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected the send")
	}
	return sdkgo.NewMutationBranch(SendPackageBranchSent, SentPackage{ID: input.PackageID, Status: "SENT"}, nil, sdkgo.Receipt{})
}

// TrashPackageOperation moves a transaction to Trash.
type TrashPackageOperation struct{ client *Client }

// TrashPackage binds the trash action.
func (client *Client) TrashPackage() TrashPackageOperation { return TrashPackageOperation{client} }

// Definition returns the generated trash contract.
func (TrashPackageOperation) Definition() sdkgo.MutationDefinition { return TrashPackageDefinition }

// IdempotencyKey identifies the absolute trash action.
func (TrashPackageOperation) IdempotencyKey(callID sdkgo.CallID, _ TrashPackageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets trashed to true for one package.
func (operation TrashPackageOperation) Invoke(call sdkgo.Call, input TrashPackageInput) sdkgo.MutationAttempt[TrashedPackage] {
	if !validPackageID(input.PackageID) {
		return packageMutationBranch[TrashedPackage]("trashPackage", TrashPackageBranchDefect, sdkgo.FailureValidation, "package ID is invalid")
	}
	return invokePackageWrite(call, operation.client, "trashPackage", http.MethodPut, "/packages/"+url.PathEscape(input.PackageID), []byte(`{"trashed":true}`), TrashPackageBranchTrashed, TrashPackageBranchNotFound, TrashPackageBranchProviderRejected, TrashPackageBranchInvalidResponse, TrashPackageBranchDefect, TrashedPackage{ID: input.PackageID})
}

// DeletePackageOperation permanently deletes one transaction.
type DeletePackageOperation struct{ client *Client }

// DeletePackage binds permanent deletion.
func (client *Client) DeletePackage() DeletePackageOperation { return DeletePackageOperation{client} }

// Definition returns the generated deletion contract.
func (DeletePackageOperation) Definition() sdkgo.MutationDefinition { return DeletePackageDefinition }

// IdempotencyKey identifies repeated deletion of the same package.
func (DeletePackageOperation) IdempotencyKey(callID sdkgo.CallID, _ DeletePackageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke permanently deletes the selected package.
func (operation DeletePackageOperation) Invoke(call sdkgo.Call, input DeletePackageInput) sdkgo.MutationAttempt[DeletedPackage] {
	if !validPackageID(input.PackageID) {
		return packageMutationBranch[DeletedPackage]("deletePackage", DeletePackageBranchDefect, sdkgo.FailureValidation, "package ID is invalid")
	}
	return invokePackageWrite(call, operation.client, "deletePackage", http.MethodDelete, "/packages/"+url.PathEscape(input.PackageID), nil, DeletePackageBranchDeleted, DeletePackageBranchNotFound, DeletePackageBranchProviderRejected, DeletePackageBranchInvalidResponse, DeletePackageBranchDefect, DeletedPackage{ID: input.PackageID})
}
