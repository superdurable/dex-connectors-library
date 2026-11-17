// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerio

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// AddCustomersToSegmentInput identifies people to add to a manual segment.
type AddCustomersToSegmentInput struct {
	// SegmentID is the positive integer under Segments > selected segment > Usage.
	SegmentID int64 `json:"segmentID"`
	// IDType is id, email, cio_id, or phone; blank means id.
	IDType string `json:"idType,omitempty"`
	// CustomerIDs are 1 to 1000 identifiers of the chosen type.
	CustomerIDs []string `json:"customerIDs"`
}

// RemoveCustomersFromSegmentInput identifies people to remove from a manual segment.
type RemoveCustomersFromSegmentInput struct {
	// SegmentID is the positive integer under Segments > selected segment > Usage.
	SegmentID int64 `json:"segmentID"`
	// IDType is id, email, or cio_id; blank means id.
	IDType string `json:"idType,omitempty"`
	// CustomerIDs are 1 to 1000 identifiers of the chosen type.
	CustomerIDs []string `json:"customerIDs"`
}

// SegmentMembershipOutput identifies the accepted segment membership request.
type SegmentMembershipOutput struct {
	// SegmentID is the submitted manual segment ID.
	SegmentID int64 `json:"segmentID"`
	// CustomerIDs are the submitted customer identifiers.
	CustomerIDs []string `json:"customerIDs"`
}

// AddCustomersToSegmentOperation adds existing people to a manual segment.
type AddCustomersToSegmentOperation struct{ client *Client }

// RemoveCustomersFromSegmentOperation removes people from a manual segment.
type RemoveCustomersFromSegmentOperation struct{ client *Client }

// AddCustomersToSegment binds this mutation to the client.
func (client *Client) AddCustomersToSegment() AddCustomersToSegmentOperation {
	return AddCustomersToSegmentOperation{client}
}

// RemoveCustomersFromSegment binds this mutation to the client.
func (client *Client) RemoveCustomersFromSegment() RemoveCustomersFromSegmentOperation {
	return RemoveCustomersFromSegmentOperation{client}
}

// Definition returns the generated addition contract.
func (AddCustomersToSegmentOperation) Definition() sdkgo.MutationDefinition {
	return AddCustomersToSegmentDefinition
}

// Definition returns the generated removal contract.
func (RemoveCustomersFromSegmentOperation) Definition() sdkgo.MutationDefinition {
	return RemoveCustomersFromSegmentDefinition
}

// IdempotencyKey correlates attempts; Customer.io does not consume it.
func (AddCustomersToSegmentOperation) IdempotencyKey(id sdkgo.CallID, _ AddCustomersToSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey correlates attempts; Customer.io does not consume it.
func (RemoveCustomersFromSegmentOperation) IdempotencyKey(id sdkgo.CallID, _ RemoveCustomersFromSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke makes the submitted people members of the manual segment.
func (operation AddCustomersToSegmentOperation) Invoke(call sdkgo.Call, input AddCustomersToSegmentInput) sdkgo.MutationAttempt[SegmentMembershipOutput] {
	return operation.client.mutateSegmentMembership(call, "addCustomersToSegment", input.SegmentID, input.IDType, input.CustomerIDs, "add_customers", true, AddCustomersToSegmentBranchAdded, AddCustomersToSegmentBranchProviderRejected, AddCustomersToSegmentBranchDefect)
}

// Invoke removes the submitted people from the manual segment.
func (operation RemoveCustomersFromSegmentOperation) Invoke(call sdkgo.Call, input RemoveCustomersFromSegmentInput) sdkgo.MutationAttempt[SegmentMembershipOutput] {
	return operation.client.mutateSegmentMembership(call, "removeCustomersFromSegment", input.SegmentID, input.IDType, input.CustomerIDs, "remove_customers", false, RemoveCustomersFromSegmentBranchRemoved, RemoveCustomersFromSegmentBranchProviderRejected, RemoveCustomersFromSegmentBranchDefect)
}

func (client *Client) mutateSegmentMembership(call sdkgo.Call, operation string, segmentID int64, idType string, customerIDs []string, action string, canUsePhone bool, successBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[SegmentMembershipOutput] {
	if !validSegmentMembershipInput(segmentID, idType, customerIDs, canUsePhone) {
		return segmentBranch(operation, defectBranch, sdkgo.FailureValidation, "invalid manual segment or customer identifiers")
	}
	payload, err := json.Marshal(struct {
		IDs []string `json:"ids"`
	}{customerIDs})
	if err != nil {
		return segmentBranch(operation, defectBranch, sdkgo.FailureValidation, "customer identifiers cannot be encoded")
	}
	path := "/api/v1/segments/" + strconv.FormatInt(segmentID, 10) + "/" + action
	if idType != "" && idType != "id" {
		path += "?" + url.Values{"id_type": {idType}}.Encode()
	}
	response := client.doTrackRequest(call, http.MethodPost, path, payload)
	if errors.Is(response.err, errCredential) {
		return segmentBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Customer.io Track credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[SegmentMembershipOutput](trackWriteFailure(operation, sdkgo.FailureTransport, "segment membership request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[SegmentMembershipOutput](trackWriteFailure(operation, sdkgo.FailureRateLimit, "Customer.io rate limited the membership request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[SegmentMembershipOutput](trackWriteFailure(operation, sdkgo.FailureAvailability, "segment membership outcome is unknown"), response.retryAfter)
	}
	if response.status == http.StatusUnauthorized {
		return segmentBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Customer.io rejected Track credentials")
	}
	if response.status != http.StatusOK {
		return segmentBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "Customer.io rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[SegmentMembershipOutput](trackWriteFailure(operation, sdkgo.FailureTransport, "segment membership response failed"), 0)
	}
	return sdkgo.NewMutationBranch(successBranch, SegmentMembershipOutput{SegmentID: segmentID, CustomerIDs: customerIDs}, nil, sdkgo.Receipt{})
}

func validSegmentMembershipInput(segmentID int64, idType string, customerIDs []string, canUsePhone bool) bool {
	if segmentID <= 0 || segmentID > 1<<31-1 || len(customerIDs) == 0 || len(customerIDs) > 1000 {
		return false
	}
	if idType != "" && idType != "id" && idType != "email" && idType != "cio_id" && !(canUsePhone && idType == "phone") {
		return false
	}
	for _, customerID := range customerIDs {
		if customerID == "" || len(customerID) > 150 || !utf8.ValidString(customerID) || strings.TrimSpace(customerID) != customerID || strings.ContainsAny(customerID, "\r\n\t") {
			return false
		}
	}
	return true
}

func segmentBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SegmentMembershipOutput] {
	failure := trackWriteFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, SegmentMembershipOutput{}, &failure, sdkgo.Receipt{})
}
