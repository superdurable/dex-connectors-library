// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactImportsInput filters a contact-import page.
type ListContactImportsInput struct {
	// Status is queued, in_progress, completed, or failed; blank includes all.
	Status string `json:"status,omitempty"`
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive import ID cursor.
	After string `json:"after,omitempty"`
}

// GetContactImportInput selects a contact import.
type GetContactImportInput struct {
	// ID is the import ID.
	ID string `json:"id"`
}

// ContactImportCounts reports import row results.
type ContactImportCounts struct {
	// Total is the number of rows.
	Total int `json:"total"`
	// Created is the number of new contacts.
	Created int `json:"created"`
	// Updated is the number of changed contacts.
	Updated int `json:"updated"`
	// Skipped is the number of ignored rows.
	Skipped int `json:"skipped"`
	// Failed is the number of failed rows.
	Failed int `json:"failed"`
}

// ContactImport describes a queued or completed contact import.
type ContactImport struct {
	// ID is the import ID.
	ID string `json:"id"`
	// Status is queued, in_progress, completed, or failed.
	Status string `json:"status"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
	// CompletedAt is the optional completion timestamp.
	CompletedAt string `json:"completed_at"`
	// Counts reports imported row outcomes.
	Counts ContactImportCounts `json:"counts"`
}

// ContactImportPage contains one import page.
type ContactImportPage struct {
	// Data contains contact import summaries.
	Data []ContactImport `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// ListContactImportsOperation lists contact imports.
type ListContactImportsOperation struct{ client *Client }

// GetContactImportOperation retrieves one contact import.
type GetContactImportOperation struct{ client *Client }

// ListContactImports binds import listing to this client.
func (client *Client) ListContactImports() ListContactImportsOperation {
	return ListContactImportsOperation{client}
}

// GetContactImport binds import retrieval to this client.
func (client *Client) GetContactImport() GetContactImportOperation {
	return GetContactImportOperation{client}
}

// Definition returns the generated listContactImports contract.
func (ListContactImportsOperation) Definition() sdkgo.QueryDefinition {
	return ListContactImportsDefinition
}

// Definition returns the generated getContactImport contract.
func (GetContactImportOperation) Definition() sdkgo.QueryDefinition {
	return GetContactImportDefinition
}

// Invoke lists contact imports with status and cursor filters.
func (operation ListContactImportsOperation) Invoke(call sdkgo.Call, input ListContactImportsInput) sdkgo.QueryAttempt[ContactImportPage] {
	if input.Status != "" && !oneOf(input.Status, "queued", "in_progress", "completed", "failed") {
		return queryFailure[ContactImportPage]("listContactImports", ListContactImportsBranchDefect, sdkgo.FailureValidation, "import status is invalid")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[ContactImportPage]("listContactImports", ListContactImportsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.Status != "" {
		if query == "" {
			query = "?status=" + url.QueryEscape(input.Status)
		} else {
			query += "&status=" + url.QueryEscape(input.Status)
		}
	}
	return runQuery(operation.client, call, "listContactImports", "/contacts/imports"+query, ListContactImportsBranchListed, "", ListContactImportsBranchProviderRejected, ListContactImportsBranchInvalidResponse, ListContactImportsBranchDefect, decodeContactImportPage)
}

// Invoke retrieves one contact import by ID.
func (operation GetContactImportOperation) Invoke(call sdkgo.Call, input GetContactImportInput) sdkgo.QueryAttempt[ContactImport] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[ContactImport]("getContactImport", GetContactImportBranchDefect, sdkgo.FailureValidation, "import ID is required")
	}
	return runQuery(operation.client, call, "getContactImport", "/contacts/imports/"+url.PathEscape(input.ID), GetContactImportBranchFound, GetContactImportBranchNotFound, GetContactImportBranchProviderRejected, GetContactImportBranchInvalidResponse, GetContactImportBranchDefect, decodeContactImport)
}
func decodeContactImport(body []byte) (ContactImport, error) {
	var value ContactImport
	err := json.Unmarshal(body, &value)
	if err == nil && (value.ID == "" || value.Status == "") {
		err = fmt.Errorf("contact import fields missing")
	}
	return value, err
}
func decodeContactImportPage(body []byte) (ContactImportPage, error) {
	var page ContactImportPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("contact import page data missing")
	}
	return page, err
}
