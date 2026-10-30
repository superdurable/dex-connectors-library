// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package smartsheet implements read-only Smartsheet API Queries.
package smartsheet

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Smartsheet.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Smartsheet credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Smartsheet base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListSheetsInput selects one page of accessible sheets.
type ListSheetsInput struct {
	// Page is the 1-based page number; zero selects page one.
	Page int `json:"page,omitempty"`
	// PageSize is records per page; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
}

// SheetSummary is a sheet identity and display name.
type SheetSummary struct {
	// ID is the provider-assigned resource identity.
	ID int64 `json:"id"`
	// Name is the resource display name.
	Name string `json:"name"`
}

// SheetPage is one Smartsheet list page.
type SheetPage struct {
	// Sheets contains the accessible sheets on this page.
	Sheets []SheetSummary `json:"sheets"`
	// PageNumber is the provider-returned 1-based page number.
	PageNumber int `json:"pageNumber"`
	// TotalCount counts all accessible matching sheets.
	TotalCount int `json:"totalCount"`
}

// GetSheetInput identifies one sheet.
type GetSheetInput struct {
	// SheetID is the positive numeric Smartsheet sheet ID.
	SheetID int64 `json:"sheetId"`
}

// SheetColumn is a column identity and title.
type SheetColumn struct {
	// ID is the provider-assigned resource identity.
	ID int64 `json:"id"`
	// Title is the column heading.
	Title string `json:"title"`
}

// SheetRow is a sheet row with its cell values.
type SheetRow struct {
	// ID is the provider-assigned resource identity.
	ID int64 `json:"id"`
	// Cells preserves each row cell as provider JSON.
	Cells []json.RawMessage `json:"cells"`
}

// Sheet contains the requested sheet's metadata, columns, and rows.
type Sheet struct {
	// ID is the provider-assigned resource identity.
	ID int64 `json:"id"`
	// Name is the resource display name.
	Name string `json:"name"`
	// Columns describes the sheet columns.
	Columns []SheetColumn `json:"columns"`
	// Rows contains the sheet rows returned by the API.
	Rows []SheetRow `json:"rows"`
}

// ListSheets returns the bound listSheets Query.
func (client *Client) ListSheets() ListSheetsOperation { return ListSheetsOperation{client} }

// ListSheetsOperation performs one Smartsheet read.
type ListSheetsOperation struct{ client *Client }

// Definition returns the manifest-generated operation contract.
func (ListSheetsOperation) Definition() sdkgo.QueryDefinition { return ListSheetsDefinition }

// Invoke validates input and classifies the HTTP result.
func (operation ListSheetsOperation) Invoke(call sdkgo.Call, input ListSheetsInput) sdkgo.QueryAttempt[SheetPage] {
	if input.Page < 0 || input.PageSize < 0 || input.PageSize > 1000 {
		return failedBranch[SheetPage]("listSheets", ListSheetsBranchDefect, sdkgo.FailureValidation, "page or pageSize is invalid")
	}
	page := max(input.Page, 1)
	size := input.PageSize
	if size == 0 {
		size = 100
	}
	path := "/sheets?page=" + strconv.Itoa(page) + "&pageSize=" + strconv.Itoa(size)
	return invokeGet(operation.client, call, "listSheets", path, ListSheetsBranchListed, ListSheetsBranchProviderRejected, decodeListSheets)
}
func decodeListSheets(body []byte) (SheetPage, error) {
	var wire struct {
		Data []SheetSummary `json:"data"`
		// PageNumber is the provider-returned 1-based page number.
		PageNumber int `json:"pageNumber"`
		// TotalCount counts all accessible matching sheets.
		TotalCount int `json:"totalCount"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SheetPage{}, err
	}
	if wire.Data == nil || wire.PageNumber < 1 || wire.TotalCount < 0 {
		return SheetPage{}, fmt.Errorf("incomplete sheet page")
	}
	return SheetPage{Sheets: wire.Data, PageNumber: wire.PageNumber, TotalCount: wire.TotalCount}, nil
}

// GetSheet returns the bound getSheet Query.
func (client *Client) GetSheet() GetSheetOperation { return GetSheetOperation{client} }

// GetSheetOperation performs one Smartsheet read.
type GetSheetOperation struct{ client *Client }

// Definition returns the manifest-generated operation contract.
func (GetSheetOperation) Definition() sdkgo.QueryDefinition { return GetSheetDefinition }

// Invoke validates input and classifies the HTTP result.
func (operation GetSheetOperation) Invoke(call sdkgo.Call, input GetSheetInput) sdkgo.QueryAttempt[Sheet] {
	if input.SheetID <= 0 {
		return failedBranch[Sheet]("getSheet", GetSheetBranchDefect, sdkgo.FailureValidation, "sheetId must be positive")
	}
	path := "/sheets/" + strconv.FormatInt(input.SheetID, 10)
	return invokeGet(operation.client, call, "getSheet", path, GetSheetBranchFound, GetSheetBranchNotFound, decodeGetSheet)
}
func decodeGetSheet(body []byte) (Sheet, error) {
	var sheet Sheet
	if err := json.Unmarshal(body, &sheet); err != nil {
		return Sheet{}, err
	}
	if sheet.ID <= 0 || sheet.Name == "" || sheet.Columns == nil || sheet.Rows == nil {
		return Sheet{}, fmt.Errorf("incomplete sheet")
	}
	return sheet, nil
}

func invokeGet[T any](client *Client, call sdkgo.Call, operationID, path string, successBranch, notFoundBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureLocalDefect, "request cannot be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider request failed", 0)
	}
	// Closing a read response cannot change its outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		switch {
		case response.StatusCode == http.StatusTooManyRequests:
			return retryBranch[T](operationID, sdkgo.FailureRateLimit, "provider rate limit", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError:
			return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == http.StatusNotFound && notFoundBranch != "providerRejected":
			return failedBranch[T](operationID, notFoundBranch, sdkgo.FailureNotFound, "provider resource was not found")
		case response.StatusCode == http.StatusUnauthorized:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthentication, "provider rejected the API key")
		case response.StatusCode == http.StatusForbidden:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthorization, "provider denied the request")
		default:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureProviderRejection, "provider rejected HTTP "+strconv.Itoa(response.StatusCode))
		}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureResponseTooLarge, "provider response exceeded configured limit")
	}
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider response was interrupted", 0)
	}
	value, err := decode(body)
	if err != nil {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureProtocol, "provider returned unusable JSON")
	}
	return sdkgo.NewQueryBranch(successBranch, value, nil, sdkgo.Receipt{})
}
func failedBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "smartsheet", Operation: operationID, Message: message}
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func retryBranch[T any](operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "smartsheet", Operation: operationID, Message: message}
	return sdkgo.NewQueryRetry[T](failure, after)
}
