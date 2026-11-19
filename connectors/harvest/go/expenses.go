// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListExpensesInput selects one cursor-paginated expense page.
type ListExpensesInput struct {
	// Cursor is the opaque cursor returned by the prior page.
	Cursor string `json:"cursor,omitempty"`
	// PerPage is from 1 to 2000; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
	// ProjectID optionally filters by project.
	ProjectID int64 `json:"projectId,omitempty"`
}

// GetExpenseInput identifies an expense by Harvest ID.
type GetExpenseInput struct {
	ID int64 `json:"id"`
}

// CreateExpenseInput creates a non-receipt expense.
type CreateExpenseInput struct {
	// ProjectID identifies the project charged.
	ProjectID int64 `json:"projectId"`
	// CategoryID identifies the expense category.
	CategoryID int64 `json:"categoryId"`
	// SpentDate is the ISO date when the expense occurred.
	SpentDate string `json:"spentDate"`
	// TotalCost is a positive amount in the client's currency.
	TotalCost float64 `json:"totalCost"`
	// Notes describe the expense.
	Notes string `json:"notes,omitempty"`
	// Billable optionally sets whether the expense is billed to the client.
	Billable *bool `json:"billable,omitempty"`
}

// UpdateExpenseInput replaces supplied expense fields.
type UpdateExpenseInput struct {
	// ID is the positive Harvest expense ID.
	ID int64 `json:"id"`
	// SpentDate replaces the occurrence date in ISO form.
	SpentDate *string `json:"spentDate,omitempty"`
	// TotalCost replaces the positive cost in client currency.
	TotalCost *float64 `json:"totalCost,omitempty"`
	// Notes replaces the description; blank clears it.
	Notes *string `json:"notes,omitempty"`
	// Billable replaces whether the expense is billed.
	Billable *bool `json:"billable,omitempty"`
}

// DeleteExpenseInput identifies an expense to delete.
type DeleteExpenseInput struct {
	ID int64 `json:"id"`
}

// ExpenseRecord summarizes a Harvest expense.
type ExpenseRecord struct {
	// ID is assigned by Harvest.
	ID int64 `json:"id"`
	// ProjectID identifies the charged project.
	ProjectID int64 `json:"projectId"`
	// CategoryID identifies the expense category.
	CategoryID int64 `json:"categoryId"`
	// Notes describe the expense.
	Notes string `json:"notes"`
	// TotalCost is the cost in the client's currency.
	TotalCost float64 `json:"totalCost"`
	// SpentDate is the date the expense occurred.
	SpentDate string `json:"spentDate"`
	// Billable reports whether the expense is billable.
	Billable bool `json:"billable"`
}

// ExpensePage contains one expense page and its next cursor.
type ExpensePage struct {
	// Expenses are the records in this page.
	Expenses []ExpenseRecord `json:"expenses"`
	// NextCursor is blank on the final page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// DeletedExpense identifies a deleted expense.
type DeletedExpense struct {
	ID int64 `json:"id"`
}

// ListExpensesOperation reads an expense page.
type ListExpensesOperation struct{ client *Client }

// ListExpenses binds the expense-list Query to this client.
func (client *Client) ListExpenses() ListExpensesOperation { return ListExpensesOperation{client} }

// Definition returns the generated Query contract.
func (ListExpensesOperation) Definition() sdkgo.QueryDefinition { return ListExpensesDefinition }

// Invoke validates pagination and reads expenses.
func (operation ListExpensesOperation) Invoke(call sdkgo.Call, input ListExpensesInput) sdkgo.QueryAttempt[ExpensePage] {
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[ExpensePage]("listExpenses", ListExpensesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.ProjectID < 0 {
		return failedBranch[ExpensePage]("listExpenses", ListExpensesBranchDefect, sdkgo.FailureValidation, "project ID must not be negative")
	}
	path := harvestPagePath("/expenses", input.Cursor, input.PerPage)
	if input.ProjectID > 0 {
		path += "&project_id=" + strconv.FormatInt(input.ProjectID, 10)
	}
	return invokeGet(operation.client, call, "listExpenses", path, ListExpensesBranchListed, ListExpensesBranchProviderRejected, decodeExpensePage)
}

// GetExpenseOperation reads one expense.
type GetExpenseOperation struct{ client *Client }

// GetExpense binds the expense lookup Query to this client.
func (client *Client) GetExpense() GetExpenseOperation { return GetExpenseOperation{client} }

// Definition returns the generated Query contract.
func (GetExpenseOperation) Definition() sdkgo.QueryDefinition { return GetExpenseDefinition }

// Invoke reads an expense by ID.
func (operation GetExpenseOperation) Invoke(call sdkgo.Call, input GetExpenseInput) sdkgo.QueryAttempt[ExpenseRecord] {
	if input.ID <= 0 {
		return failedBranch[ExpenseRecord]("getExpense", GetExpenseBranchDefect, sdkgo.FailureValidation, "expense ID must be positive")
	}
	return invokeGet(operation.client, call, "getExpense", "/expenses/"+strconv.FormatInt(input.ID, 10), GetExpenseBranchFound, GetExpenseBranchNotFound, decodeExpenseRecord)
}

// CreateExpenseOperation creates an expense with bounded repeat.
type CreateExpenseOperation struct{ client *Client }

// CreateExpense binds expense creation to this client.
func (client *Client) CreateExpense() CreateExpenseOperation { return CreateExpenseOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateExpenseOperation) Definition() sdkgo.MutationDefinition { return CreateExpenseDefinition }

// IdempotencyKey records the Dex call identity; Harvest does not deduplicate this POST.
func (CreateExpenseOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateExpenseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an expense under the limited dispatch fence.
func (operation CreateExpenseOperation) Invoke(call sdkgo.Call, input CreateExpenseInput) sdkgo.MutationAttempt[ExpenseRecord] {
	if input.ProjectID <= 0 || input.CategoryID <= 0 || !validInvoiceDate(input.SpentDate) || input.SpentDate == "" || !validExpenseCost(input.TotalCost) || len(input.Notes) > 10000 {
		return harvestMutationFailure[ExpenseRecord]("createExpense", CreateExpenseBranchDefect, sdkgo.FailureValidation, "expense project, category, date, or cost is invalid")
	}
	payload, err := json.Marshal(struct {
		ProjectID  int64   `json:"project_id"`
		CategoryID int64   `json:"expense_category_id"`
		SpentDate  string  `json:"spent_date"`
		TotalCost  float64 `json:"total_cost"`
		Notes      string  `json:"notes,omitempty"`
		Billable   *bool   `json:"billable,omitempty"`
	}{input.ProjectID, input.CategoryID, input.SpentDate, input.TotalCost, input.Notes, input.Billable})
	if err != nil {
		return harvestMutationFailure[ExpenseRecord]("createExpense", CreateExpenseBranchDefect, sdkgo.FailureValidation, "expense input cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createExpense", CreateExpenseDefinition.StepDefaults.ExecuteMethodTimeout, "/expenses", payload, CreateExpenseBranchCreated, CreateExpenseBranchProviderRejected, CreateExpenseBranchDefect, decodeExpenseRecord, func(expense ExpenseRecord) string { return strconv.FormatInt(expense.ID, 10) })
}

// UpdateExpenseOperation changes absolute expense fields.
type UpdateExpenseOperation struct{ client *Client }

// UpdateExpense binds the expense update Mutation to this client.
func (client *Client) UpdateExpense() UpdateExpenseOperation { return UpdateExpenseOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateExpenseOperation) Definition() sdkgo.MutationDefinition { return UpdateExpenseDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateExpenseOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateExpenseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies a repeat-safe expense PATCH.
func (operation UpdateExpenseOperation) Invoke(call sdkgo.Call, input UpdateExpenseInput) sdkgo.MutationAttempt[ExpenseRecord] {
	if input.ID <= 0 || input.SpentDate == nil && input.TotalCost == nil && input.Notes == nil && input.Billable == nil || input.SpentDate != nil && (!validInvoiceDate(*input.SpentDate) || *input.SpentDate == "") || input.TotalCost != nil && !validExpenseCost(*input.TotalCost) || input.Notes != nil && len(*input.Notes) > 10000 {
		return harvestMutationFailure[ExpenseRecord]("updateExpense", UpdateExpenseBranchDefect, sdkgo.FailureValidation, "expense ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		SpentDate *string  `json:"spent_date,omitempty"`
		TotalCost *float64 `json:"total_cost,omitempty"`
		Notes     *string  `json:"notes,omitempty"`
		Billable  *bool    `json:"billable,omitempty"`
	}{input.SpentDate, input.TotalCost, input.Notes, input.Billable})
	if err != nil {
		return harvestMutationFailure[ExpenseRecord]("updateExpense", UpdateExpenseBranchDefect, sdkgo.FailureValidation, "expense update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateExpense", http.MethodPatch, "/expenses/"+strconv.FormatInt(input.ID, 10), payload, UpdateExpenseBranchUpdated, UpdateExpenseBranchNotFound, UpdateExpenseBranchProviderRejected, decodeExpenseRecord)
}

// DeleteExpenseOperation deletes an expense.
type DeleteExpenseOperation struct{ client *Client }

// DeleteExpense binds the expense deletion Mutation to this client.
func (client *Client) DeleteExpense() DeleteExpenseOperation { return DeleteExpenseOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteExpenseOperation) Definition() sdkgo.MutationDefinition { return DeleteExpenseDefinition }

// IdempotencyKey records the stable Dex call identity.
func (DeleteExpenseOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteExpenseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a repeat-safe expense DELETE.
func (operation DeleteExpenseOperation) Invoke(call sdkgo.Call, input DeleteExpenseInput) sdkgo.MutationAttempt[DeletedExpense] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedExpense]("deleteExpense", DeleteExpenseBranchDefect, sdkgo.FailureValidation, "expense ID must be positive")
	}
	decode := func(_ []byte) (DeletedExpense, error) { return DeletedExpense{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteExpense", http.MethodDelete, "/expenses/"+strconv.FormatInt(input.ID, 10), nil, DeleteExpenseBranchDeleted, DeleteExpenseBranchNotFound, DeleteExpenseBranchProviderRejected, decode)
}

func validExpenseCost(cost float64) bool {
	return cost > 0 && !math.IsNaN(cost) && !math.IsInf(cost, 0)
}
func decodeExpenseRecord(body []byte) (ExpenseRecord, error) {
	var wire struct {
		ID      int64 `json:"id"`
		Project struct {
			ID int64 `json:"id"`
		} `json:"project"`
		Category struct {
			ID int64 `json:"id"`
		} `json:"expense_category"`
		Notes     string  `json:"notes"`
		TotalCost float64 `json:"total_cost"`
		SpentDate string  `json:"spent_date"`
		Billable  bool    `json:"billable"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ExpenseRecord{}, err
	}
	if wire.ID <= 0 || wire.Project.ID <= 0 || wire.Category.ID <= 0 || !validInvoiceDate(wire.SpentDate) || wire.SpentDate == "" || math.IsNaN(wire.TotalCost) || math.IsInf(wire.TotalCost, 0) {
		return ExpenseRecord{}, fmt.Errorf("missing expense identity")
	}
	return ExpenseRecord{ID: wire.ID, ProjectID: wire.Project.ID, CategoryID: wire.Category.ID, Notes: wire.Notes, TotalCost: wire.TotalCost, SpentDate: wire.SpentDate, Billable: wire.Billable}, nil
}
func decodeExpensePage(body []byte) (ExpensePage, error) {
	var wire struct {
		Expenses []json.RawMessage `json:"expenses"`
		Links    struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ExpensePage{}, err
	}
	if wire.Expenses == nil {
		return ExpensePage{}, fmt.Errorf("missing expenses")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/expenses")
	if err != nil {
		return ExpensePage{}, err
	}
	page := ExpensePage{Expenses: make([]ExpenseRecord, 0, len(wire.Expenses)), NextCursor: next}
	for _, raw := range wire.Expenses {
		expense, err := decodeExpenseRecord(raw)
		if err != nil {
			return ExpensePage{}, err
		}
		page.Expenses = append(page.Expenses, expense)
	}
	return page, nil
}
