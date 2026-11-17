// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CreateTransferInput creates an unfunded transfer using an authenticated quote.
type CreateTransferInput struct {
	// QuoteID is the authenticated Wise quote UUID.
	QuoteID string `json:"quoteId"`
	// RecipientID is the positive decimal recipient account ID.
	RecipientID string `json:"recipientId"`
	// Reference is optional recipient-visible text, subject to corridor rules.
	Reference string `json:"reference,omitempty"`
	// TransferPurpose is an optional purpose code from Wise transfer requirements.
	TransferPurpose string `json:"transferPurpose,omitempty"`
}

// CancelTransferInput selects an unfunded transfer for cancellation.
type CancelTransferInput struct {
	// TransferID is the positive decimal transfer ID.
	TransferID string `json:"transferId"`
}

// CreateTransferOperation creates an unfunded Wise transfer.
type CreateTransferOperation struct{ client *Client }

// CreateTransfer binds the Mutation to this client.
func (client *Client) CreateTransfer() CreateTransferOperation {
	return CreateTransferOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreateTransferOperation) Definition() sdkgo.MutationDefinition { return CreateTransferDefinition }

// IdempotencyKey reuses the stable Call ID as Wise's customer transaction ID.
func (CreateTransferOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTransferInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one unfunded transfer and retries unknown outcomes with the same transaction ID.
func (operation CreateTransferOperation) Invoke(call sdkgo.Call, input CreateTransferInput) sdkgo.MutationAttempt[TransferStatus] {
	if !quoteUUID.MatchString(input.QuoteID) || !validTransferID(input.RecipientID) || !quoteUUID.MatchString(string(call.IdempotencyKey)) || len(input.Reference) > 200 || len(input.TransferPurpose) > 100 {
		return wiseMutationBranch[TransferStatus]("createTransfer", "defect", sdkgo.FailureValidation, "quote, recipient, reference, purpose, or call ID is invalid")
	}
	recipientID, _ := strconv.ParseInt(input.RecipientID, 10, 64)
	request := map[string]any{"quoteUuid": input.QuoteID, "targetAccount": recipientID, "customerTransactionId": string(call.IdempotencyKey)}
	details := map[string]string{}
	if input.Reference != "" {
		details["reference"] = input.Reference
	}
	if input.TransferPurpose != "" {
		details["transferPurpose"] = input.TransferPurpose
	}
	if len(details) > 0 {
		request["details"] = details
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return wiseMutationBranch[TransferStatus]("createTransfer", "defect", sdkgo.FailureLocalDefect, "Wise transfer request could not be encoded")
	}
	response := operation.client.send(call, http.MethodPost, "/transfers", requestBody)
	if retry, ok := wiseMutationRetry[TransferStatus]("createTransfer", response); ok {
		return retry
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return wiseMutationBranch[TransferStatus]("createTransfer", "providerRejected", sdkgo.FailureProviderRejection, "Wise rejected transfer creation")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return wiseMutationBranch[TransferStatus]("createTransfer", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wise transfer response exceeded the limit")
	}
	transfer, err := decodeTransferCreated(response.body)
	if err != nil {
		return wiseMutationBranch[TransferStatus]("createTransfer", "invalidResponse", sdkgo.FailureProtocol, "Wise returned invalid transfer JSON")
	}
	return sdkgo.NewMutationBranch(CreateTransferBranchCreated, transfer, nil, sdkgo.Receipt{})
}

// CancelTransferOperation cancels an unfunded Wise transfer.
type CancelTransferOperation struct{ client *Client }

// CancelTransfer binds the Mutation to this client.
func (client *Client) CancelTransfer() CancelTransferOperation {
	return CancelTransferOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CancelTransferOperation) Definition() sdkgo.MutationDefinition { return CancelTransferDefinition }

// IdempotencyKey identifies retries of the same cancellation.
func (CancelTransferOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelTransferInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke cancels a transfer and confirms a cancelled state after a conflict.
func (operation CancelTransferOperation) Invoke(call sdkgo.Call, input CancelTransferInput) sdkgo.MutationAttempt[TransferStatus] {
	if !validTransferID(input.TransferID) {
		return wiseMutationBranch[TransferStatus]("cancelTransfer", "defect", sdkgo.FailureValidation, "transfer ID must be a positive decimal integer")
	}
	path := "/transfers/" + url.PathEscape(input.TransferID) + "/cancel"
	response := operation.client.send(call, http.MethodPut, path, nil)
	if retry, ok := wiseMutationRetry[TransferStatus]("cancelTransfer", response); ok {
		return retry
	}
	if response.status == http.StatusNotFound {
		return wiseMutationBranch[TransferStatus]("cancelTransfer", "notFound", sdkgo.FailureNotFound, "Wise did not expose the transfer")
	}
	if response.status == http.StatusConflict {
		readBack := operation.client.get(call, "/transfers/"+url.PathEscape(input.TransferID))
		if readBack.err == nil && readBack.status == http.StatusOK {
			transfer, err := decodeTransfer(readBack.body, input.TransferID)
			if err == nil && transfer.Status == "cancelled" {
				return sdkgo.NewMutationBranch(CancelTransferBranchCancelled, transfer, nil, sdkgo.Receipt{})
			}
		}
		return wiseMutationBranch[TransferStatus]("cancelTransfer", "providerRejected", sdkgo.FailureProviderRejection, "Wise refused transfer cancellation")
	}
	if response.status != http.StatusOK {
		return wiseMutationBranch[TransferStatus]("cancelTransfer", "providerRejected", sdkgo.FailureProviderRejection, "Wise refused transfer cancellation")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return wiseMutationBranch[TransferStatus]("cancelTransfer", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wise transfer response exceeded the limit")
	}
	transfer, err := decodeTransfer(response.body, input.TransferID)
	if err != nil || transfer.Status != "cancelled" {
		return wiseMutationBranch[TransferStatus]("cancelTransfer", "invalidResponse", sdkgo.FailureProtocol, "Wise returned an invalid cancellation response")
	}
	return sdkgo.NewMutationBranch(CancelTransferBranchCancelled, transfer, nil, sdkgo.Receipt{})
}

func wiseMutationRetry[T any](operationID string, response providerResponse) (sdkgo.MutationAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return wiseMutationBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "Wise credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "wise", Operation: operationID, Message: "Wise request failed"}, 0), true
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "wise", Operation: operationID, Message: "Wise is temporarily unavailable"}, response.retryAfter), true
	}
	if response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "wise", Operation: operationID, Message: "Wise response was interrupted"}, 0), true
	}
	var zero sdkgo.MutationAttempt[T]
	return zero, false
}

func wiseMutationBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wise", Operation: operationID, Message: message}
	var zero T
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func decodeTransferCreated(body []byte) (TransferStatus, error) {
	var identity struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &identity); err != nil {
		return TransferStatus{}, err
	}
	if identity.ID <= 0 {
		return TransferStatus{}, fmt.Errorf("transfer ID missing")
	}
	transfer, err := decodeTransfer(body, strconv.FormatInt(identity.ID, 10))
	if err != nil || strings.TrimSpace(transfer.Status) == "" {
		return TransferStatus{}, fmt.Errorf("transfer response missing required fields")
	}
	return transfer, nil
}
