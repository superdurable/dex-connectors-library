// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CreatePaymentInput records a cash or external payment without persisting payment tokens in a Dex Flow.
type CreatePaymentInput struct {
	// SourceID is CASH or EXTERNAL; payment tokens must not be persisted in Flow input.
	SourceID string `json:"sourceId"`
	// AmountMoney is the positive payment amount in minor currency units.
	AmountMoney Money `json:"amountMoney"`
}

// UpdatePaymentInput changes the amount of an approved payment.
type UpdatePaymentInput struct {
	// PaymentID identifies an approved Square payment.
	PaymentID string `json:"paymentId"`
	// AmountMoney is the replacement amount in minor currency units.
	AmountMoney Money `json:"amountMoney"`
}

// CancelPaymentInput identifies an approved payment to void.
type CancelPaymentInput struct {
	// PaymentID identifies the approved Square payment.
	PaymentID string `json:"paymentId"`
}

// CompletePaymentInput identifies an approved payment to capture.
type CompletePaymentInput struct {
	// PaymentID identifies the approved Square payment.
	PaymentID string `json:"paymentId"`
}

// CreatePaymentOperation creates a Square payment.
type CreatePaymentOperation struct{ client *Client }

// UpdatePaymentOperation updates an approved Square payment.
type UpdatePaymentOperation struct{ client *Client }

// CancelPaymentOperation voids an approved Square payment.
type CancelPaymentOperation struct{ client *Client }

// CompletePaymentOperation captures an approved Square payment.
type CompletePaymentOperation struct{ client *Client }

// CreatePayment binds the createPayment Mutation to this client.
func (client *Client) CreatePayment() CreatePaymentOperation { return CreatePaymentOperation{client} }

// UpdatePayment binds the updatePayment Mutation to this client.
func (client *Client) UpdatePayment() UpdatePaymentOperation { return UpdatePaymentOperation{client} }

// CancelPayment binds the cancelPayment Mutation to this client.
func (client *Client) CancelPayment() CancelPaymentOperation { return CancelPaymentOperation{client} }

// CompletePayment binds the completePayment Mutation to this client.
func (client *Client) CompletePayment() CompletePaymentOperation {
	return CompletePaymentOperation{client}
}

// Definition returns the manifest-generated contract.
func (CreatePaymentOperation) Definition() sdkgo.MutationDefinition { return CreatePaymentDefinition }

// Definition returns the manifest-generated contract.
func (UpdatePaymentOperation) Definition() sdkgo.MutationDefinition { return UpdatePaymentDefinition }

// Definition returns the manifest-generated contract.
func (CancelPaymentOperation) Definition() sdkgo.MutationDefinition { return CancelPaymentDefinition }

// Definition returns the manifest-generated contract.
func (CompletePaymentOperation) Definition() sdkgo.MutationDefinition {
	return CompletePaymentDefinition
}

// IdempotencyKey derives a Square-compatible key from the stable Dex call ID.
func (CreatePaymentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePaymentInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey derives a Square-compatible key from the stable Dex call ID.
func (UpdatePaymentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePaymentInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey binds retries of this cancellation to one Dex call.
func (CancelPaymentOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelPaymentInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey binds retries of this capture to one Dex call.
func (CompletePaymentOperation) IdempotencyKey(callID sdkgo.CallID, _ CompletePaymentInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

func squarePaymentKey(callID sdkgo.CallID) sdkgo.IdempotencyKey {
	hash := sha256.Sum256([]byte(callID))
	return sdkgo.IdempotencyKey("dex-" + hex.EncodeToString(hash[:20]))
}

// Invoke creates a payment using the same provider key on every attempt.
func (operation CreatePaymentOperation) Invoke(call sdkgo.Call, input CreatePaymentInput) sdkgo.MutationAttempt[Payment] {
	if (input.SourceID != "CASH" && input.SourceID != "EXTERNAL") || input.AmountMoney.Amount <= 0 || input.AmountMoney.Currency == "" {
		return paymentWriteDefect("createPayment", CreatePaymentBranchDefect)
	}
	body := map[string]any{"source_id": input.SourceID, "amount_money": input.AmountMoney, "idempotency_key": call.IdempotencyKey}
	return executePaymentWrite(operation.client, call, paymentWriteRequest{"createPayment", http.MethodPost, "/payments", body, CreatePaymentBranchCreated, "", CreatePaymentBranchProviderRejected, CreatePaymentBranchInvalidResponse, CreatePaymentBranchDefect, ""})
}

// Invoke replaces the approved payment amount using the same provider key on every attempt.
func (operation UpdatePaymentOperation) Invoke(call sdkgo.Call, input UpdatePaymentInput) sdkgo.MutationAttempt[Payment] {
	if !validPathSegment(input.PaymentID) || input.AmountMoney.Amount <= 0 || input.AmountMoney.Currency == "" {
		return paymentWriteDefect("updatePayment", UpdatePaymentBranchDefect)
	}
	body := map[string]any{"payment": map[string]any{"amount_money": input.AmountMoney}, "idempotency_key": call.IdempotencyKey}
	return executePaymentWrite(operation.client, call, paymentWriteRequest{"updatePayment", http.MethodPut, "/payments/" + url.PathEscape(input.PaymentID), body, UpdatePaymentBranchUpdated, UpdatePaymentBranchNotFound, UpdatePaymentBranchProviderRejected, UpdatePaymentBranchInvalidResponse, UpdatePaymentBranchDefect, ""})
}

// Invoke voids an approved payment and confirms a lost response by reading its state.
func (operation CancelPaymentOperation) Invoke(call sdkgo.Call, input CancelPaymentInput) sdkgo.MutationAttempt[Payment] {
	if !validPathSegment(input.PaymentID) {
		return paymentWriteDefect("cancelPayment", CancelPaymentBranchDefect)
	}
	return executePaymentWrite(operation.client, call, paymentWriteRequest{"cancelPayment", http.MethodPost, "/payments/" + url.PathEscape(input.PaymentID) + "/cancel", nil, CancelPaymentBranchCanceled, CancelPaymentBranchNotFound, CancelPaymentBranchProviderRejected, CancelPaymentBranchInvalidResponse, CancelPaymentBranchDefect, "CANCELED"})
}

// Invoke captures an approved payment and confirms a lost response by reading its state.
func (operation CompletePaymentOperation) Invoke(call sdkgo.Call, input CompletePaymentInput) sdkgo.MutationAttempt[Payment] {
	if !validPathSegment(input.PaymentID) {
		return paymentWriteDefect("completePayment", CompletePaymentBranchDefect)
	}
	return executePaymentWrite(operation.client, call, paymentWriteRequest{"completePayment", http.MethodPost, "/payments/" + url.PathEscape(input.PaymentID) + "/complete", map[string]any{}, CompletePaymentBranchCompleted, CompletePaymentBranchNotFound, CompletePaymentBranchProviderRejected, CompletePaymentBranchInvalidResponse, CompletePaymentBranchDefect, "COMPLETED"})
}

type paymentWriteRequest struct {
	operation          string
	method             string
	path               string
	body               map[string]any
	successBranch      sdkgo.BranchID
	notFoundBranch     sdkgo.BranchID
	rejectedBranch     sdkgo.BranchID
	invalidBranch      sdkgo.BranchID
	defectBranch       sdkgo.BranchID
	confirmationStatus string
}

func paymentWriteDefect(operation string, branch sdkgo.BranchID) sdkgo.MutationAttempt[Payment] {
	return sdkgo.NewMutationBranch(branch, Payment{}, failureFor(operation, sdkgo.FailureValidation, "payment input is invalid"), sdkgo.Receipt{})
}

func executePaymentWrite(client *Client, call sdkgo.Call, write paymentWriteRequest) sdkgo.MutationAttempt[Payment] {
	var payload []byte
	if write.body != nil {
		var err error
		payload, err = json.Marshal(write.body)
		if err != nil {
			return paymentWriteDefect(write.operation, write.defectBranch)
		}
	}
	request, err := http.NewRequestWithContext(call.Context, write.method, client.baseURL+write.path, bytes.NewReader(payload))
	if err != nil {
		return paymentWriteDefect(write.operation, write.defectBranch)
	}
	request.Header.Set("Accept", "application/json")
	if write.body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	authorization := client.authorize(call, write.operation, request, write.defectBranch, write.rejectedBranch, write.invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Payment{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return confirmPaymentWrite(client, call, write, "provider write response was unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Payment](*failureFor(write.operation, sdkgo.FailureRateLimit, "provider rate limited the write"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusServiceUnavailable:
		return sdkgo.NewMutationRetry[Payment](*failureFor(write.operation, sdkgo.FailureAvailability, "provider write is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return confirmPaymentWrite(client, call, write, "provider write outcome is unavailable")
	case response.StatusCode == http.StatusNotFound && write.notFoundBranch != "":
		return sdkgo.NewMutationBranch(write.notFoundBranch, Payment{}, failureFor(write.operation, sdkgo.FailureNotFound, "payment was not found"), sdkgo.Receipt{})
	case response.StatusCode == http.StatusUnauthorized:
		return sdkgo.NewMutationBranch(write.rejectedBranch, Payment{}, failureFor(write.operation, sdkgo.FailureAuthentication, "provider rejected credentials"), sdkgo.Receipt{})
	case response.StatusCode == http.StatusForbidden:
		return sdkgo.NewMutationBranch(write.rejectedBranch, Payment{}, failureFor(write.operation, sdkgo.FailureAuthorization, "provider denied access"), sdkgo.Receipt{})
	case response.StatusCode < 200 || response.StatusCode >= 300:
		if write.confirmationStatus != "" && (response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusUnprocessableEntity) {
			if confirmed, payment := readPaymentWriteConfirmation(client, call, write); confirmed {
				return sdkgo.NewMutationBranch(write.successBranch, payment, nil, sdkgo.Receipt{})
			}
		}
		return sdkgo.NewMutationBranch(write.rejectedBranch, Payment{}, failureFor(write.operation, sdkgo.FailureProviderRejection, "provider rejected write with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(write.invalidBranch, Payment{}, failureFor(write.operation, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return confirmPaymentWrite(client, call, write, "provider response was interrupted")
	}
	payment, err := decodeGetPaymentResponse(body)
	if err != nil {
		return sdkgo.NewMutationBranch(write.invalidBranch, Payment{}, failureFor(write.operation, sdkgo.FailureProtocol, "provider returned invalid JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(write.successBranch, payment, nil, sdkgo.Receipt{})
}

func confirmPaymentWrite(client *Client, call sdkgo.Call, write paymentWriteRequest, message string) sdkgo.MutationAttempt[Payment] {
	if write.confirmationStatus != "" {
		if confirmed, payment := readPaymentWriteConfirmation(client, call, write); confirmed {
			return sdkgo.NewMutationBranch(write.successBranch, payment, nil, sdkgo.Receipt{})
		}
	}
	return sdkgo.NewMutationRetry[Payment](*failureFor(write.operation, sdkgo.FailureAvailability, message), 0)
}

func readPaymentWriteConfirmation(client *Client, call sdkgo.Call, write paymentWriteRequest) (bool, Payment) {
	if write.confirmationStatus != "" {
		paymentPath := write.path[:len(write.path)-len("/cancel")]
		if write.confirmationStatus == "COMPLETED" {
			paymentPath = write.path[:len(write.path)-len("/complete")]
		}
		request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+paymentPath, nil)
		if err != nil {
			return false, Payment{}
		}
		request.Header.Set("Accept", "application/json")
		if authorization := client.authorize(call, write.operation, request, write.defectBranch, write.rejectedBranch, write.invalidBranch); authorization.failure == nil {
			response, err := client.httpClient.Do(request)
			if err == nil {
				defer func() { _ = response.Body.Close() }()
				if response.StatusCode == http.StatusOK {
					body, readError := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
					if readError == nil {
						payment, decodeError := decodeGetPaymentResponse(body)
						if decodeError == nil && payment.Status == write.confirmationStatus {
							return true, payment
						}
					}
				}
			}
		}
	}
	return false, Payment{}
}
