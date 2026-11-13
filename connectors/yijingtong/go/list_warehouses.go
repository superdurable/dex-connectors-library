// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yijingtong

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListWarehousesInput selects the tenant's authorized warehouse catalog.
type ListWarehousesInput struct{}

// Warehouse identifies a Yijingtong warehouse and its country.
type Warehouse struct {
	// ID is the provider's warehouse code.
	ID string `json:"id"`
	// Name is the provider's warehouse display name.
	Name string `json:"name"`
	// Country is the provider's country display name.
	Country string `json:"country"`
}

// WarehousePage contains the bounded authorized warehouse catalog.
type WarehousePage struct {
	// Warehouses contains at most 200 records.
	Warehouses []Warehouse `json:"warehouses"`
}

// ListWarehousesOperation reads the tenant's authorized warehouse catalog.
type ListWarehousesOperation struct{ client *Client }

// Definition returns the generated warehouse-list contract.
func (ListWarehousesOperation) Definition() sdkgo.QueryDefinition { return ListWarehousesDefinition }

// Invoke queries Yijingtong and classifies provider, protocol, and transport outcomes.
func (operation ListWarehousesOperation) Invoke(call sdkgo.Call, _ ListWarehousesInput) sdkgo.QueryAttempt[WarehousePage] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validWarehouseToken(credentials.Token.Reveal()) {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureAuthentication, "Yijingtong token is unavailable or invalid")
	}
	requestBody, err := json.Marshal(map[string]string{"token": credentials.Token.Reveal()})
	if err != nil {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureValidation, "warehouse request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/hwc_api/hwc_storage_list.php", bytes.NewReader(requestBody))
	if err != nil {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureValidation, "warehouse request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return warehouseRetry(sdkgo.FailureAvailability, "Yijingtong warehouse request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return warehouseRetry(sdkgo.FailureRateLimit, "Yijingtong rate limited warehouse query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return warehouseRetry(sdkgo.FailureAvailability, "Yijingtong warehouse API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return warehouseBranch(ListWarehousesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Yijingtong response exceeded the configured limit")
	}
	if err != nil {
		return warehouseRetry(sdkgo.FailureAvailability, "Yijingtong response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return warehouseBranch(ListWarehousesBranchProviderRejected, classifyWarehouseHTTP(response.StatusCode), "Yijingtong rejected warehouse query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, isRejected, err := decodeWarehousePage(body)
	if err != nil {
		return warehouseBranch(ListWarehousesBranchInvalidResponse, sdkgo.FailureProtocol, "Yijingtong returned an invalid warehouse response")
	}
	if isRejected {
		return warehouseBranch(ListWarehousesBranchProviderRejected, sdkgo.FailureProviderRejection, "Yijingtong rejected warehouse query")
	}
	return sdkgo.NewQueryBranch(ListWarehousesBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeWarehousePage(body []byte) (WarehousePage, bool, error) {
	var wire struct {
		Error string `json:"error"`
		Data  []struct {
			ID      string `json:"nums"`
			Name    string `json:"pname"`
			Country string `json:"country"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || (wire.Error != "T" && wire.Error != "F") {
		return WarehousePage{}, false, errors.New("invalid Yijingtong warehouse JSON")
	}
	if wire.Error == "F" {
		return WarehousePage{}, true, nil
	}
	if wire.Data == nil || len(wire.Data) > 200 {
		return WarehousePage{}, false, errors.New("invalid Yijingtong warehouse catalog")
	}
	page := WarehousePage{Warehouses: make([]Warehouse, 0, len(wire.Data))}
	for _, item := range wire.Data {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" {
			return WarehousePage{}, false, errors.New("invalid Yijingtong warehouse")
		}
		page.Warehouses = append(page.Warehouses, Warehouse{ID: item.ID, Name: item.Name, Country: item.Country})
	}
	return page, false, nil
}

func validWarehouseToken(token string) bool {
	if token == "" || len(token) > 2048 || strings.TrimSpace(token) != token {
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
	failure := sdkgo.Failure{Kind: kind, Provider: "yijingtong", Operation: "listWarehouses", Message: message}
	return sdkgo.NewQueryBranch(branch, WarehousePage{}, &failure, sdkgo.Receipt{})
}

func warehouseRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[WarehousePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yijingtong", Operation: "listWarehouses", Message: message}
	return sdkgo.NewQueryRetry[WarehousePage](failure, delay)
}
