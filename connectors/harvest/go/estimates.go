// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListEstimatesInput selects one cursor-paginated estimate page.
type ListEstimatesInput struct {
	// Cursor is the opaque cursor returned by the prior page.
	Cursor string `json:"cursor,omitempty"`
	// PerPage is from 1 to 2000; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
	// ClientID optionally restricts estimates to one client.
	ClientID int64 `json:"clientId,omitempty"`
	// State optionally selects draft, sent, accepted, or declined estimates.
	State string `json:"state,omitempty"`
}

// GetEstimateInput identifies an estimate by Harvest ID.
type GetEstimateInput struct {
	ID int64 `json:"id"`
}

// EstimateLineItemInput describes one estimate item.
type EstimateLineItemInput struct {
	// Kind is a configured Harvest estimate item category.
	Kind string `json:"kind"`
	// Description describes the quoted work.
	Description string `json:"description,omitempty"`
	// Quantity is a positive item count or duration.
	Quantity float64 `json:"quantity"`
	// UnitPrice is a nonnegative amount in the estimate currency.
	UnitPrice float64 `json:"unitPrice"`
	// Taxed applies the estimate's first tax percentage.
	Taxed bool `json:"taxed"`
}

// CreateEstimateInput creates a draft estimate.
type CreateEstimateInput struct {
	// ClientID identifies the quoted client.
	ClientID int64 `json:"clientId"`
	// Number optionally sets the estimate number; blank lets Harvest assign one.
	Number string `json:"number,omitempty"`
	// Subject is the estimate subject.
	Subject string `json:"subject,omitempty"`
	// Notes appear on the estimate.
	Notes string `json:"notes,omitempty"`
	// LineItems are the quoted items.
	LineItems []EstimateLineItemInput `json:"lineItems"`
}

// UpdateEstimateInput replaces supplied estimate header fields.
type UpdateEstimateInput struct {
	// ID is the positive Harvest estimate ID.
	ID int64 `json:"id"`
	// Number replaces the estimate number when supplied.
	Number *string `json:"number,omitempty"`
	// Subject replaces the subject when supplied.
	Subject *string `json:"subject,omitempty"`
	// Notes replaces the notes when supplied; blank clears them.
	Notes *string `json:"notes,omitempty"`
}

// DeleteEstimateInput identifies an estimate to delete.
type DeleteEstimateInput struct {
	ID int64 `json:"id"`
}

// EstimateRecord contains summary fields for a Harvest estimate.
type EstimateRecord struct {
	// ID is assigned by Harvest.
	ID int64 `json:"id"`
	// ClientID identifies the quoted client.
	ClientID int64 `json:"clientId"`
	// Number is the estimate number.
	Number string `json:"number"`
	// Subject is the estimate subject.
	Subject string `json:"subject"`
	// State is draft, sent, accepted, or declined.
	State string `json:"state"`
	// Amount is the estimate total in its currency.
	Amount float64 `json:"amount"`
	// IssueDate is the issue date in YYYY-MM-DD form.
	IssueDate string `json:"issueDate"`
}

// EstimatePage contains one estimate page and its next cursor.
type EstimatePage struct {
	// Estimates are the records in this page.
	Estimates []EstimateRecord `json:"estimates"`
	// NextCursor is blank on the final page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// DeletedEstimate identifies a deleted estimate.
type DeletedEstimate struct {
	ID int64 `json:"id"`
}

// ListEstimatesOperation reads an estimate page.
type ListEstimatesOperation struct{ client *Client }

// ListEstimates binds the estimate-list Query to this client.
func (client *Client) ListEstimates() ListEstimatesOperation { return ListEstimatesOperation{client} }

// Definition returns the generated Query contract.
func (ListEstimatesOperation) Definition() sdkgo.QueryDefinition { return ListEstimatesDefinition }

// Invoke validates filters and reads estimates.
func (operation ListEstimatesOperation) Invoke(call sdkgo.Call, input ListEstimatesInput) sdkgo.QueryAttempt[EstimatePage] {
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[EstimatePage]("listEstimates", ListEstimatesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.ClientID < 0 || !validEstimateState(input.State) {
		return failedBranch[EstimatePage]("listEstimates", ListEstimatesBranchDefect, sdkgo.FailureValidation, "estimate filter is invalid")
	}
	path := harvestPagePath("/estimates", input.Cursor, input.PerPage)
	if input.ClientID > 0 {
		path += "&client_id=" + strconv.FormatInt(input.ClientID, 10)
	}
	if input.State != "" {
		path += "&state=" + input.State
	}
	return invokeGet(operation.client, call, "listEstimates", path, ListEstimatesBranchListed, ListEstimatesBranchProviderRejected, decodeEstimatePage)
}

// GetEstimateOperation reads one estimate.
type GetEstimateOperation struct{ client *Client }

// GetEstimate binds the estimate lookup Query to this client.
func (client *Client) GetEstimate() GetEstimateOperation { return GetEstimateOperation{client} }

// Definition returns the generated Query contract.
func (GetEstimateOperation) Definition() sdkgo.QueryDefinition { return GetEstimateDefinition }

// Invoke reads an estimate by ID.
func (operation GetEstimateOperation) Invoke(call sdkgo.Call, input GetEstimateInput) sdkgo.QueryAttempt[EstimateRecord] {
	if input.ID <= 0 {
		return failedBranch[EstimateRecord]("getEstimate", GetEstimateBranchDefect, sdkgo.FailureValidation, "estimate ID must be positive")
	}
	return invokeGet(operation.client, call, "getEstimate", "/estimates/"+strconv.FormatInt(input.ID, 10), GetEstimateBranchFound, GetEstimateBranchNotFound, decodeEstimateRecord)
}

// CreateEstimateOperation creates an estimate with bounded repeat.
type CreateEstimateOperation struct{ client *Client }

// CreateEstimate binds estimate creation to this client.
func (client *Client) CreateEstimate() CreateEstimateOperation {
	return CreateEstimateOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateEstimateOperation) Definition() sdkgo.MutationDefinition { return CreateEstimateDefinition }

// IdempotencyKey records the Dex call identity; Harvest does not deduplicate this POST.
func (CreateEstimateOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEstimateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an estimate under the limited dispatch fence.
func (operation CreateEstimateOperation) Invoke(call sdkgo.Call, input CreateEstimateInput) sdkgo.MutationAttempt[EstimateRecord] {
	if input.ClientID <= 0 || len(input.LineItems) == 0 || len(input.LineItems) > 100 || !validEstimateHeader(input.Number, input.Subject, input.Notes) {
		return harvestMutationFailure[EstimateRecord]("createEstimate", CreateEstimateBranchDefect, sdkgo.FailureValidation, "estimate client, header, or line items are invalid")
	}
	type lineWire struct {
		Kind        string  `json:"kind"`
		Description string  `json:"description,omitempty"`
		Quantity    float64 `json:"quantity"`
		UnitPrice   float64 `json:"unit_price"`
		Taxed       bool    `json:"taxed"`
	}
	lines := make([]lineWire, 0, len(input.LineItems))
	for _, item := range input.LineItems {
		if !validEstimateLineItem(item) {
			return harvestMutationFailure[EstimateRecord]("createEstimate", CreateEstimateBranchDefect, sdkgo.FailureValidation, "estimate line item is invalid")
		}
		lines = append(lines, lineWire{item.Kind, item.Description, item.Quantity, item.UnitPrice, item.Taxed})
	}
	payload, err := json.Marshal(struct {
		ClientID  int64      `json:"client_id"`
		Number    string     `json:"number,omitempty"`
		Subject   string     `json:"subject,omitempty"`
		Notes     string     `json:"notes,omitempty"`
		LineItems []lineWire `json:"line_items"`
	}{input.ClientID, input.Number, input.Subject, input.Notes, lines})
	if err != nil {
		return harvestMutationFailure[EstimateRecord]("createEstimate", CreateEstimateBranchDefect, sdkgo.FailureValidation, "estimate input cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createEstimate", CreateEstimateDefinition.StepDefaults.ExecuteMethodTimeout, "/estimates", payload, CreateEstimateBranchCreated, CreateEstimateBranchProviderRejected, CreateEstimateBranchDefect, decodeEstimateRecord, func(estimate EstimateRecord) string { return strconv.FormatInt(estimate.ID, 10) })
}

// UpdateEstimateOperation changes absolute estimate header fields.
type UpdateEstimateOperation struct{ client *Client }

// UpdateEstimate binds the estimate update Mutation to this client.
func (client *Client) UpdateEstimate() UpdateEstimateOperation {
	return UpdateEstimateOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateEstimateOperation) Definition() sdkgo.MutationDefinition { return UpdateEstimateDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateEstimateOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateEstimateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies a repeat-safe estimate PATCH.
func (operation UpdateEstimateOperation) Invoke(call sdkgo.Call, input UpdateEstimateInput) sdkgo.MutationAttempt[EstimateRecord] {
	if input.ID <= 0 || input.Number == nil && input.Subject == nil && input.Notes == nil || input.Number != nil && len(*input.Number) > 100 || input.Subject != nil && len(*input.Subject) > 500 || input.Notes != nil && len(*input.Notes) > 10000 {
		return harvestMutationFailure[EstimateRecord]("updateEstimate", UpdateEstimateBranchDefect, sdkgo.FailureValidation, "estimate ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Number  *string `json:"number,omitempty"`
		Subject *string `json:"subject,omitempty"`
		Notes   *string `json:"notes,omitempty"`
	}{input.Number, input.Subject, input.Notes})
	if err != nil {
		return harvestMutationFailure[EstimateRecord]("updateEstimate", UpdateEstimateBranchDefect, sdkgo.FailureValidation, "estimate update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateEstimate", http.MethodPatch, "/estimates/"+strconv.FormatInt(input.ID, 10), payload, UpdateEstimateBranchUpdated, UpdateEstimateBranchNotFound, UpdateEstimateBranchProviderRejected, decodeEstimateRecord)
}

// DeleteEstimateOperation deletes an estimate.
type DeleteEstimateOperation struct{ client *Client }

// DeleteEstimate binds the estimate deletion Mutation to this client.
func (client *Client) DeleteEstimate() DeleteEstimateOperation {
	return DeleteEstimateOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteEstimateOperation) Definition() sdkgo.MutationDefinition { return DeleteEstimateDefinition }

// IdempotencyKey records the stable Dex call identity.
func (DeleteEstimateOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteEstimateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a repeat-safe estimate DELETE.
func (operation DeleteEstimateOperation) Invoke(call sdkgo.Call, input DeleteEstimateInput) sdkgo.MutationAttempt[DeletedEstimate] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedEstimate]("deleteEstimate", DeleteEstimateBranchDefect, sdkgo.FailureValidation, "estimate ID must be positive")
	}
	decode := func(_ []byte) (DeletedEstimate, error) { return DeletedEstimate{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteEstimate", http.MethodDelete, "/estimates/"+strconv.FormatInt(input.ID, 10), nil, DeleteEstimateBranchDeleted, DeleteEstimateBranchNotFound, DeleteEstimateBranchProviderRejected, decode)
}

func validEstimateState(state string) bool {
	return state == "" || state == "draft" || state == "sent" || state == "accepted" || state == "declined"
}
func validEstimateHeader(number, subject, notes string) bool {
	return len(number) <= 100 && len(subject) <= 500 && len(notes) <= 10000
}
func validEstimateLineItem(item EstimateLineItemInput) bool {
	return strings.TrimSpace(item.Kind) != "" && len(item.Kind) <= 255 && len(item.Description) <= 5000 && item.Quantity > 0 && !math.IsNaN(item.Quantity) && !math.IsInf(item.Quantity, 0) && item.UnitPrice >= 0 && !math.IsNaN(item.UnitPrice) && !math.IsInf(item.UnitPrice, 0)
}
func decodeEstimateRecord(body []byte) (EstimateRecord, error) {
	var wire struct {
		ID     int64 `json:"id"`
		Client struct {
			ID int64 `json:"id"`
		} `json:"client"`
		Number    string  `json:"number"`
		Subject   string  `json:"subject"`
		State     string  `json:"state"`
		Amount    float64 `json:"amount"`
		IssueDate string  `json:"issue_date"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EstimateRecord{}, err
	}
	if wire.ID <= 0 || wire.Client.ID <= 0 || !validEstimateState(wire.State) || wire.State == "" || math.IsNaN(wire.Amount) || math.IsInf(wire.Amount, 0) {
		return EstimateRecord{}, fmt.Errorf("missing estimate identity")
	}
	return EstimateRecord{ID: wire.ID, ClientID: wire.Client.ID, Number: wire.Number, Subject: wire.Subject, State: wire.State, Amount: wire.Amount, IssueDate: wire.IssueDate}, nil
}
func decodeEstimatePage(body []byte) (EstimatePage, error) {
	var wire struct {
		Estimates []json.RawMessage `json:"estimates"`
		Links     struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EstimatePage{}, err
	}
	if wire.Estimates == nil {
		return EstimatePage{}, fmt.Errorf("missing estimates")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/estimates")
	if err != nil {
		return EstimatePage{}, err
	}
	page := EstimatePage{Estimates: make([]EstimateRecord, 0, len(wire.Estimates)), NextCursor: next}
	for _, raw := range wire.Estimates {
		estimate, err := decodeEstimateRecord(raw)
		if err != nil {
			return EstimatePage{}, err
		}
		page.Estimates = append(page.Estimates, estimate)
	}
	return page, nil
}
