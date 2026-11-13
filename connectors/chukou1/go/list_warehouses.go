// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chukou1

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ListWarehousesInput selects the authorized overseas warehouse catalog.
type ListWarehousesInput struct{}

// Warehouse identifies a Chukou1 overseas warehouse and its country.
type Warehouse struct {
	// ID is the provider's warehouse code.
	ID string `json:"id"`
	// Name is the provider's warehouse display name.
	Name string `json:"name"`
	// Country is the provider's country display name.
	Country string `json:"country"`
	// CountryCode is the provider's two-letter country code.
	CountryCode string `json:"countryCode"`
}

// WarehousePage contains the bounded authorized warehouse catalog.
type WarehousePage struct {
	// Warehouses contains at most 200 records.
	Warehouses []Warehouse `json:"warehouses"`
}

// ListWarehousesOperation reads the authorized warehouse catalog.
type ListWarehousesOperation struct{ client *Client }

// Definition returns the generated warehouse-list contract.
func (ListWarehousesOperation) Definition() sdkgo.QueryDefinition { return ListWarehousesDefinition }

// Invoke queries Chukou1 and classifies provider, protocol, and transport outcomes.
func (operation ListWarehousesOperation) Invoke(call sdkgo.Call, _ ListWarehousesInput) sdkgo.QueryAttempt[WarehousePage] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAccessToken(credentials.AccessToken.Reveal()) {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureAuthentication, "Chukou1 access token is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/warehouses", nil)
	if err != nil {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureValidation, "warehouse request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return warehouseRetry(sdkgo.FailureAvailability, "Chukou1 warehouse request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return warehouseRetry(sdkgo.FailureRateLimit, "Chukou1 rate limited warehouse query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return warehouseRetry(sdkgo.FailureAvailability, "Chukou1 warehouse API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return warehouseBranch(ListWarehousesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Chukou1 response exceeded the configured limit")
	}
	if err != nil {
		return warehouseRetry(sdkgo.FailureAvailability, "Chukou1 response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return warehouseBranch(ListWarehousesBranchProviderRejected, classifyWarehouseHTTP(response.StatusCode), "Chukou1 rejected warehouse query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, err := decodeWarehousePage(body)
	if err != nil {
		return warehouseBranch(ListWarehousesBranchInvalidResponse, sdkgo.FailureProtocol, "Chukou1 returned an invalid warehouse response")
	}
	return sdkgo.NewQueryBranch(ListWarehousesBranchListed, page, nil, sdkgo.Receipt{})
}
func decodeWarehousePage(body []byte) (WarehousePage, error) {
	var wire []struct {
		ID          string `json:"WarehouseId"`
		Name        string `json:"WarehouseName"`
		Country     string `json:"Country"`
		CountryCode string `json:"CountryCode"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire == nil || len(wire) > 200 {
		return WarehousePage{}, errors.New("invalid Chukou1 warehouse JSON")
	}
	page := WarehousePage{Warehouses: make([]Warehouse, 0, len(wire))}
	for _, item := range wire {
		if item.ID == "" || item.Name == "" || item.CountryCode == "" {
			return WarehousePage{}, errors.New("invalid Chukou1 warehouse")
		}
		page.Warehouses = append(page.Warehouses, Warehouse{ID: item.ID, Name: item.Name, Country: item.Country, CountryCode: item.CountryCode})
	}
	return page, nil
}
func validAccessToken(token string) bool {
	if token == "" || len(token) > 2048 || strings.TrimSpace(token) != token || strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return false
	}
	for _, character := range token {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyWarehouseHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 412:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func warehouseBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WarehousePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "chukou1", Operation: "listWarehouses", Message: message}
	return sdkgo.NewQueryBranch(branch, WarehousePage{}, &failure, sdkgo.Receipt{})
}
func warehouseRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[WarehousePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "chukou1", Operation: "listWarehouses", Message: message}
	return sdkgo.NewQueryRetry[WarehousePage](failure, delay)
}
