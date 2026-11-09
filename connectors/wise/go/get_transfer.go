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

// GetTransferInput selects a transfer by its non-secret Wise ID.
type GetTransferInput struct {
	// TransferID is the ID shown in Wise Transfers or returned by the API.
	TransferID string `json:"transferId"`
}

// TransferStatus excludes recipient, customer, and payment details.
type TransferStatus struct {
	// ID is the Wise transfer ID.
	ID string `json:"id"`
	// SourceCurrency is the source ISO 4217 currency code.
	SourceCurrency string `json:"sourceCurrency"`
	// Status is the transfer workflow state such as incoming_payment_waiting.
	Status string `json:"status"`
}

// GetTransferOperation reads one transfer's identity and state.
type GetTransferOperation struct{ client *Client }

// GetTransfer binds the Query to this client.
func (client *Client) GetTransfer() GetTransferOperation {
	return GetTransferOperation{client}
}

// Definition returns the manifest-generated Query transfer.
func (GetTransferOperation) Definition() sdkgo.QueryDefinition { return GetTransferDefinition }

// Invoke reads one transfer and classifies retryable and definitive outcomes.
func (operation GetTransferOperation) Invoke(call sdkgo.Call, input GetTransferInput) sdkgo.QueryAttempt[TransferStatus] {
	if !validTransferID(input.TransferID) {
		return transferBranch(GetTransferBranchDefect, sdkgo.FailureValidation, "transfer ID must be a positive decimal integer")
	}
	response := operation.client.get(call, "/transfers/"+url.PathEscape(input.TransferID))
	if errors.Is(response.err, errCredential) {
		return transferBranch(GetTransferBranchDefect, sdkgo.FailureAuthentication, "Wise credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TransferStatus](transferFailure(sdkgo.FailureTransport, "Wise request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TransferStatus](transferFailure(sdkgo.FailureRateLimit, "Wise rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TransferStatus](transferFailure(sdkgo.FailureAvailability, "Wise is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return transferBranch(GetTransferBranchNotFound, sdkgo.FailureNotFound, "Wise did not expose the transfer")
	}
	if response.status != http.StatusOK {
		return transferBranch(GetTransferBranchProviderRejected, sdkgo.FailureProviderRejection, "Wise rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return transferBranch(GetTransferBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Wise response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TransferStatus](transferFailure(sdkgo.FailureTransport, "Wise response was interrupted"), 0)
	}
	transferStatus, err := decodeTransfer(response.body, input.TransferID)
	if err != nil {
		return transferBranch(GetTransferBranchInvalidResponse, sdkgo.FailureProtocol, "Wise returned invalid transfer JSON")
	}
	return sdkgo.NewQueryBranch(GetTransferBranchFound, transferStatus, nil, sdkgo.Receipt{})
}

func validTransferID(transferID string) bool {
	if len(transferID) == 0 || len(transferID) > 19 || transferID[0] == '0' {
		return false
	}
	for _, character := range transferID {
		if character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	parsedID, err := strconv.ParseInt(transferID, 10, 64)
	return err == nil && parsedID <= 9007199254740991
}

func decodeTransfer(body []byte, requestedTransferID string) (TransferStatus, error) {
	var wire struct {
		ID             int64  `json:"id"`
		SourceCurrency string `json:"sourceCurrency"`
		Status         string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TransferStatus{}, err
	}
	if strconv.FormatInt(wire.ID, 10) != requestedTransferID || len(wire.SourceCurrency) != 3 || strings.TrimSpace(wire.Status) == "" {
		return TransferStatus{}, fmt.Errorf("transfer identity, source currency, or status missing")
	}
	return TransferStatus{ID: requestedTransferID, SourceCurrency: wire.SourceCurrency, Status: wire.Status}, nil
}

func transferFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "wise", Operation: "getTransfer", Message: message}
}

func transferBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TransferStatus] {
	failure := transferFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TransferStatus{}, &failure, sdkgo.Receipt{})
}
