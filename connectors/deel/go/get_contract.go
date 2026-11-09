// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deel

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

// GetContractInput selects a contract by its non-secret Deel ID.
type GetContractInput struct {
	// ContractID is the ID shown in Deel Contracts or returned by the API.
	ContractID string `json:"contractId"`
}

// ContractStatus excludes worker, client, and compensation details.
type ContractStatus struct {
	// ID is the Deel contract ID.
	ID string `json:"id"`
	// Type is Deel's contract type such as ongoing_time_based.
	Type string `json:"type"`
	// Status is the contract workflow state such as in_progress.
	Status string `json:"status"`
}

// GetContractOperation reads one contract's identity and state.
type GetContractOperation struct{ client *Client }

// GetContract binds the Query to this client.
func (client *Client) GetContract() GetContractOperation {
	return GetContractOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetContractOperation) Definition() sdkgo.QueryDefinition { return GetContractDefinition }

// Invoke reads one contract and classifies retryable and definitive outcomes.
func (operation GetContractOperation) Invoke(call sdkgo.Call, input GetContractInput) sdkgo.QueryAttempt[ContractStatus] {
	if !validContractID(input.ContractID) {
		return contractBranch(GetContractBranchDefect, sdkgo.FailureValidation, "contract ID must be 1–128 URL-safe characters")
	}
	response := operation.client.get(call, "/contracts/"+url.PathEscape(input.ContractID))
	if errors.Is(response.err, errCredential) {
		return contractBranch(GetContractBranchDefect, sdkgo.FailureAuthentication, "Deel credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ContractStatus](contractFailure(sdkgo.FailureTransport, "Deel request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ContractStatus](contractFailure(sdkgo.FailureRateLimit, "Deel rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ContractStatus](contractFailure(sdkgo.FailureAvailability, "Deel is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return contractBranch(GetContractBranchNotFound, sdkgo.FailureNotFound, "Deel did not expose the contract")
	}
	if response.status != http.StatusOK {
		return contractBranch(GetContractBranchProviderRejected, sdkgo.FailureProviderRejection, "Deel rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return contractBranch(GetContractBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Deel response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ContractStatus](contractFailure(sdkgo.FailureTransport, "Deel response was interrupted"), 0)
	}
	contractStatus, err := decodeContract(response.body, input.ContractID)
	if err != nil {
		return contractBranch(GetContractBranchInvalidResponse, sdkgo.FailureProtocol, "Deel returned invalid contract JSON")
	}
	return sdkgo.NewQueryBranch(GetContractBranchFound, contractStatus, nil, sdkgo.Receipt{})
}

func validContractID(contractID string) bool {
	if len(contractID) == 0 || len(contractID) > 128 {
		return false
	}
	for _, character := range contractID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodeContract(body []byte, requestedContractID string) (ContractStatus, error) {
	var wire struct {
		Data struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContractStatus{}, err
	}
	if wire.Data.ID != requestedContractID || strings.TrimSpace(wire.Data.Type) == "" || strings.TrimSpace(wire.Data.Status) == "" {
		return ContractStatus{}, fmt.Errorf("contract identity, type, or status missing")
	}
	return ContractStatus{ID: wire.Data.ID, Type: wire.Data.Type, Status: wire.Data.Status}, nil
}

func contractFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "deel", Operation: "getContract", Message: message}
}

func contractBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ContractStatus] {
	failure := contractFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ContractStatus{}, &failure, sdkgo.Receipt{})
}
