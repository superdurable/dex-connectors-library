// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deel

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListContractsInput selects one page of visible contracts.
type ListContractsInput struct {
	// AfterCursor is the previous page's opaque cursor; blank starts at the first page.
	AfterCursor string `json:"afterCursor,omitempty"`
	// Limit is 1 through 100; zero uses Deel's default of 50.
	Limit int `json:"limit,omitempty"`
}

// ContractPage contains contract identities and workflow states without personal details.
type ContractPage struct {
	// Contracts is the current page, possibly empty.
	Contracts []ContractStatus `json:"contracts"`
	// NextCursor is blank when Deel returns no continuation.
	NextCursor string `json:"nextCursor"`
}

// ListContractTasksInput selects tasks for one contract.
type ListContractTasksInput struct {
	// ContractID is the non-secret contract ID from Deel Contracts.
	ContractID string `json:"contractId"`
}

// ContractTask is a task identity and workflow status without amounts or attachments.
type ContractTask struct {
	// ID is the task's opaque provider ID.
	ID string `json:"id"`
	// Status is the provider's task workflow status.
	Status string `json:"status"`
}

// ContractTaskList is the list of tasks for one contract.
type ContractTaskList struct {
	// Tasks excludes amounts, descriptions, and attachments.
	Tasks []ContractTask `json:"tasks"`
}

// ListContractAmendmentsInput selects one page of a contract's amendments.
type ListContractAmendmentsInput struct {
	// ContractID is the contract ID from Deel Contracts.
	ContractID string `json:"contractId"`
	// Cursor is the previous page's opaque cursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit is 1 through 100; zero uses Deel's default of 20.
	Limit int `json:"limit,omitempty"`
}

// ContractAmendment is an amendment identity and workflow state without compensation details.
type ContractAmendment struct {
	// ID is the amendment's provider ID.
	ID string `json:"id"`
	// Status is the amendment workflow status.
	Status string `json:"status"`
	// SignStatus is the signature workflow status.
	SignStatus string `json:"signStatus"`
}

// ContractAmendmentPage is one bounded page of amendments.
type ContractAmendmentPage struct {
	// Amendments contains this page, possibly empty.
	Amendments []ContractAmendment `json:"amendments"`
	// NextCursor is blank when there are no further pages.
	NextCursor string `json:"nextCursor"`
}

// ListTerminationReasonsInput selects one page of standard reasons.
type ListTerminationReasonsInput struct {
	// Cursor is the previous page's opaque cursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit is 1 through 100; zero uses Deel's default of 20.
	Limit int `json:"limit,omitempty"`
}

// TerminationReason is a provider-defined reason identifier and label.
type TerminationReason struct {
	// ID is the reason's provider ID.
	ID string `json:"id"`
	// Name is the reason label.
	Name string `json:"name"`
	// Frequency is Deel's frequency category.
	Frequency string `json:"frequency"`
}

// TerminationReasonPage is one page of standard reasons.
type TerminationReasonPage struct {
	// Reasons contains this page, possibly empty.
	Reasons []TerminationReason `json:"reasons"`
	// NextCursor is blank when there are no further pages.
	NextCursor string `json:"nextCursor"`
}

// ListContractsOperation reads one page of contract summaries.
type ListContractsOperation struct{ client *Client }

// ListContractTasksOperation reads task statuses for one contract.
type ListContractTasksOperation struct{ client *Client }

// ListContractAmendmentsOperation reads amendment statuses for one contract.
type ListContractAmendmentsOperation struct{ client *Client }

// ListTerminationReasonsOperation reads standard reason labels.
type ListTerminationReasonsOperation struct{ client *Client }

// ListContracts binds the query to this client.
func (client *Client) ListContracts() ListContractsOperation { return ListContractsOperation{client} }

// ListContractTasks binds the query to this client.
func (client *Client) ListContractTasks() ListContractTasksOperation {
	return ListContractTasksOperation{client}
}

// ListContractAmendments binds the query to this client.
func (client *Client) ListContractAmendments() ListContractAmendmentsOperation {
	return ListContractAmendmentsOperation{client}
}

// ListTerminationReasons binds the query to this client.
func (client *Client) ListTerminationReasons() ListTerminationReasonsOperation {
	return ListTerminationReasonsOperation{client}
}

// Definition returns the generated listContracts query definition.
func (ListContractsOperation) Definition() sdkgo.QueryDefinition { return ListContractsDefinition }

// Definition returns the generated listContractTasks query definition.
func (ListContractTasksOperation) Definition() sdkgo.QueryDefinition {
	return ListContractTasksDefinition
}

// Definition returns the generated listContractAmendments query definition.
func (ListContractAmendmentsOperation) Definition() sdkgo.QueryDefinition {
	return ListContractAmendmentsDefinition
}

// Definition returns the generated listTerminationReasons query definition.
func (ListTerminationReasonsOperation) Definition() sdkgo.QueryDefinition {
	return ListTerminationReasonsDefinition
}

// Invoke lists one page and projects only contract IDs, types, and statuses.
func (operation ListContractsOperation) Invoke(call sdkgo.Call, input ListContractsInput) sdkgo.QueryAttempt[ContractPage] {
	if !validCursor(input.AfterCursor) || input.Limit < 0 || input.Limit > 100 {
		return deelQueryBranch[ContractPage](ListContractsBranchDefect, "listContracts", sdkgo.FailureValidation, "invalid cursor or limit")
	}
	query := url.Values{}
	if input.AfterCursor != "" {
		query.Set("after_cursor", input.AfterCursor)
	}
	if input.Limit != 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	response := operation.client.get(call, deelPath("/contracts", query))
	if attempt, handled := classifyDeelQuery[ContractPage](response, "listContracts", ListContractsBranchProviderRejected, ListContractsBranchInvalidResponse, ListContractsBranchDefect, ""); handled {
		return attempt
	}
	var wire struct {
		Data []ContractStatus `json:"data"`
		Page struct {
			Cursor string `json:"cursor"`
		} `json:"page"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || !validCursor(wire.Page.Cursor) || len(wire.Data) > 100 {
		return deelQueryBranch[ContractPage](ListContractsBranchInvalidResponse, "listContracts", sdkgo.FailureProtocol, "Deel returned an invalid contract page")
	}
	for _, contract := range wire.Data {
		if !validContractID(contract.ID) || strings.TrimSpace(contract.Type) == "" || strings.TrimSpace(contract.Status) == "" {
			return deelQueryBranch[ContractPage](ListContractsBranchInvalidResponse, "listContracts", sdkgo.FailureProtocol, "Deel returned an invalid contract summary")
		}
	}
	return sdkgo.NewQueryBranch(ListContractsBranchListed, ContractPage{Contracts: wire.Data, NextCursor: wire.Page.Cursor}, nil, sdkgo.Receipt{})
}

// Invoke lists task IDs and statuses while excluding amounts, attachments, and descriptions.
func (operation ListContractTasksOperation) Invoke(call sdkgo.Call, input ListContractTasksInput) sdkgo.QueryAttempt[ContractTaskList] {
	if !validContractID(input.ContractID) {
		return deelQueryBranch[ContractTaskList](ListContractTasksBranchDefect, "listContractTasks", sdkgo.FailureValidation, "contract ID must be URL-safe")
	}
	response := operation.client.get(call, "/contracts/"+url.PathEscape(input.ContractID)+"/tasks")
	if attempt, handled := classifyDeelQuery[ContractTaskList](response, "listContractTasks", ListContractTasksBranchProviderRejected, ListContractTasksBranchInvalidResponse, ListContractTasksBranchDefect, ListContractTasksBranchNotFound); handled {
		return attempt
	}
	var wire struct {
		Data []ContractTask `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Data) > 1000 {
		return deelQueryBranch[ContractTaskList](ListContractTasksBranchInvalidResponse, "listContractTasks", sdkgo.FailureProtocol, "Deel returned an invalid task list")
	}
	for _, task := range wire.Data {
		if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Status) == "" {
			return deelQueryBranch[ContractTaskList](ListContractTasksBranchInvalidResponse, "listContractTasks", sdkgo.FailureProtocol, "Deel returned an invalid task")
		}
	}
	return sdkgo.NewQueryBranch(ListContractTasksBranchListed, ContractTaskList{Tasks: wire.Data}, nil, sdkgo.Receipt{})
}

// Invoke lists amendment IDs and statuses without compensation details.
func (operation ListContractAmendmentsOperation) Invoke(call sdkgo.Call, input ListContractAmendmentsInput) sdkgo.QueryAttempt[ContractAmendmentPage] {
	if !validContractID(input.ContractID) || !validCursor(input.Cursor) || input.Limit < 0 || input.Limit > 100 {
		return deelQueryBranch[ContractAmendmentPage](ListContractAmendmentsBranchDefect, "listContractAmendments", sdkgo.FailureValidation, "invalid contract ID, cursor, or limit")
	}
	query := url.Values{}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	if input.Limit != 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	response := operation.client.get(call, deelPath("/contracts/"+url.PathEscape(input.ContractID)+"/amendments", query))
	if attempt, handled := classifyDeelQuery[ContractAmendmentPage](response, "listContractAmendments", ListContractAmendmentsBranchProviderRejected, ListContractAmendmentsBranchInvalidResponse, ListContractAmendmentsBranchDefect, ListContractAmendmentsBranchNotFound); handled {
		return attempt
	}
	var wire struct {
		Data []struct {
			ID         string `json:"id"`
			Status     string `json:"status"`
			SignStatus string `json:"sign_status"`
		} `json:"data"`
		Cursor  string `json:"cursor"`
		HasMore bool   `json:"has_more"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Data) > 100 || !validCursor(wire.Cursor) || wire.HasMore && wire.Cursor == "" {
		return deelQueryBranch[ContractAmendmentPage](ListContractAmendmentsBranchInvalidResponse, "listContractAmendments", sdkgo.FailureProtocol, "Deel returned an invalid amendment page")
	}
	page := ContractAmendmentPage{Amendments: make([]ContractAmendment, 0, len(wire.Data))}
	if wire.HasMore {
		page.NextCursor = wire.Cursor
	}
	for _, amendment := range wire.Data {
		if strings.TrimSpace(amendment.ID) == "" || strings.TrimSpace(amendment.Status) == "" {
			return deelQueryBranch[ContractAmendmentPage](ListContractAmendmentsBranchInvalidResponse, "listContractAmendments", sdkgo.FailureProtocol, "Deel returned an invalid amendment")
		}
		page.Amendments = append(page.Amendments, ContractAmendment{ID: amendment.ID, Status: amendment.Status, SignStatus: amendment.SignStatus})
	}
	return sdkgo.NewQueryBranch(ListContractAmendmentsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke lists standard termination reasons without starting termination.
func (operation ListTerminationReasonsOperation) Invoke(call sdkgo.Call, input ListTerminationReasonsInput) sdkgo.QueryAttempt[TerminationReasonPage] {
	if !validCursor(input.Cursor) || input.Limit < 0 || input.Limit > 100 {
		return deelQueryBranch[TerminationReasonPage](ListTerminationReasonsBranchDefect, "listTerminationReasons", sdkgo.FailureValidation, "invalid cursor or limit")
	}
	query := url.Values{}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	if input.Limit != 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	response := operation.client.get(call, deelPath("/contracts/termination-reasons", query))
	if attempt, handled := classifyDeelQuery[TerminationReasonPage](response, "listTerminationReasons", ListTerminationReasonsBranchProviderRejected, ListTerminationReasonsBranchInvalidResponse, ListTerminationReasonsBranchDefect, ""); handled {
		return attempt
	}
	var wire struct {
		Data       []TerminationReason `json:"data"`
		HasMore    bool                `json:"has_more"`
		NextCursor string              `json:"next_cursor"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Data) > 100 || !validCursor(wire.NextCursor) || wire.HasMore && wire.NextCursor == "" {
		return deelQueryBranch[TerminationReasonPage](ListTerminationReasonsBranchInvalidResponse, "listTerminationReasons", sdkgo.FailureProtocol, "Deel returned an invalid reason page")
	}
	for _, reason := range wire.Data {
		if strings.TrimSpace(reason.ID) == "" || strings.TrimSpace(reason.Name) == "" {
			return deelQueryBranch[TerminationReasonPage](ListTerminationReasonsBranchInvalidResponse, "listTerminationReasons", sdkgo.FailureProtocol, "Deel returned an invalid reason")
		}
	}
	page := TerminationReasonPage{Reasons: wire.Data}
	if wire.HasMore {
		page.NextCursor = wire.NextCursor
	}
	return sdkgo.NewQueryBranch(ListTerminationReasonsBranchListed, page, nil, sdkgo.Receipt{})
}

func deelPath(path string, query url.Values) string {
	if len(query) == 0 {
		return path
	}
	return path + "?" + query.Encode()
}

func validCursor(cursor string) bool {
	return len(cursor) <= 4096 && utf8.ValidString(cursor) && !strings.ContainsAny(cursor, "\x00\r\n")
}

func deelQueryBranch[T any](branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "deel", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func classifyDeelQuery[T any](response providerResponse, operation string, rejected, invalid, defect, notFound sdkgo.BranchID) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return deelQueryBranch[T](defect, operation, sdkgo.FailureAuthentication, "Deel credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return deelQueryRetry[T](operation, sdkgo.FailureTransport, "Deel request failed"), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureRateLimit, Provider: "deel", Operation: operation, Message: "Deel rate limited the request"}, response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "deel", Operation: operation, Message: "Deel is temporarily unavailable"}, response.retryAfter), true
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return deelQueryBranch[T](notFound, operation, sdkgo.FailureNotFound, "Deel did not expose the contract"), true
	}
	if response.status != http.StatusOK {
		return deelQueryBranch[T](rejected, operation, sdkgo.FailureProviderRejection, "Deel rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return deelQueryBranch[T](invalid, operation, sdkgo.FailureResponseTooLarge, "Deel response exceeded the configured limit"), true
	}
	if response.err != nil {
		return deelQueryRetry[T](operation, sdkgo.FailureTransport, "Deel response was interrupted"), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func deelQueryRetry[T any](operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "deel", Operation: operation, Message: message}, 0)
}
