// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yanwen

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListWarehousesInput selects the authorized customer's entire warehouse list.
type ListWarehousesInput struct{}

// Warehouse identifies a Yanwen handover warehouse.
type Warehouse struct {
	// Code is the provider's stable warehouse code.
	Code string `json:"code"`
	// Name is the provider's warehouse name.
	Name string `json:"name"`
	// Area is the provider's warehouse region.
	Area string `json:"area"`
}

// WarehouseList is a bounded list of authorized handover warehouses.
type WarehouseList struct {
	// Warehouses contains at most 500 warehouses.
	Warehouses []Warehouse `json:"warehouses"`
}

// ListWarehousesOperation reads handover warehouse metadata.
type ListWarehousesOperation struct{ client *Client }

// Definition returns the generated warehouse-list Query contract.
func (ListWarehousesOperation) Definition() sdkgo.QueryDefinition { return ListWarehousesDefinition }

// Invoke signs a read-only warehouse-list request and classifies provider outcomes.
func (operation ListWarehousesOperation) Invoke(call sdkgo.Call, _ ListWarehousesInput) sdkgo.QueryAttempt[WarehouseList] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validUserID(credentials.UserID) || !validAPIToken(credentials.APIToken.Reveal()) {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureAuthentication, "Yanwen credentials are unavailable or invalid")
	}
	body := []byte("{}")
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	query := url.Values{
		"user_id":   {credentials.UserID},
		"format":    {"json"},
		"method":    {"common.warehouse.getlist"},
		"timestamp": {timestamp},
		"version":   {"V1.0"},
	}
	query.Set("sign", signWarehouseRequest(credentials.APIToken.Reveal(), credentials.UserID, string(body), timestamp))
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/api/order?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureValidation, "Yanwen warehouse request is invalid")
	}
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return warehouseRetry(sdkgo.FailureAvailability, "Yanwen warehouse request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return warehouseRetry(sdkgo.FailureRateLimit, "Yanwen rate limited warehouse query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return warehouseRetry(sdkgo.FailureAvailability, "Yanwen warehouse API is temporarily unavailable", retryAfter)
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return warehouseBranch(ListWarehousesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Yanwen warehouse response exceeded the configured limit")
	}
	if err != nil {
		return warehouseRetry(sdkgo.FailureAvailability, "Yanwen warehouse response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return warehouseBranch(ListWarehousesBranchProviderRejected, classifyWarehouseHTTP(response.StatusCode), "Yanwen rejected warehouse query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	list, isRejected, isRetry, err := decodeWarehouses(responseBody)
	if err != nil {
		return warehouseBranch(ListWarehousesBranchInvalidResponse, sdkgo.FailureProtocol, "Yanwen returned an invalid warehouse response")
	}
	if isRetry {
		return warehouseRetry(sdkgo.FailureAvailability, "Yanwen warehouse service is temporarily unavailable", retryAfter)
	}
	if isRejected {
		return warehouseBranch(ListWarehousesBranchProviderRejected, sdkgo.FailureProviderRejection, "Yanwen rejected warehouse query")
	}
	return sdkgo.NewQueryBranch(ListWarehousesBranchListed, list, nil, sdkgo.Receipt{})
}

func decodeWarehouses(body []byte) (WarehouseList, bool, bool, error) {
	var envelope struct {
		Success *bool  `json:"success"`
		Code    string `json:"code"`
		Data    []struct {
			Code string `json:"code"`
			Name string `json:"name"`
			Area string `json:"area"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Success == nil || envelope.Code == "" {
		return WarehouseList{}, false, false, errors.New("invalid Yanwen response")
	}
	if !*envelope.Success || envelope.Code != "0" {
		if strings.HasPrefix(envelope.Code, "-") {
			return WarehouseList{}, false, true, nil
		}
		return WarehouseList{}, true, false, nil
	}
	if envelope.Data == nil || len(envelope.Data) > 500 {
		return WarehouseList{}, false, false, errors.New("invalid Yanwen warehouse list")
	}
	list := WarehouseList{Warehouses: make([]Warehouse, 0, len(envelope.Data))}
	for _, item := range envelope.Data {
		if item.Code == "" || item.Name == "" || item.Area == "" {
			return WarehouseList{}, false, false, errors.New("invalid Yanwen warehouse")
		}
		list.Warehouses = append(list.Warehouses, Warehouse{Code: item.Code, Name: item.Name, Area: item.Area})
	}
	return list, false, false, nil
}

func signWarehouseRequest(token, userID, body, timestamp string) string {
	signed := token + userID + body + "json" + "common.warehouse.getlist" + timestamp + "V1.0" + token
	digest := md5.Sum([]byte(signed))
	return hex.EncodeToString(digest[:])
}
func validUserID(value string) bool {
	if value == "" || len(value) > 64 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func validAPIToken(value string) bool {
	return value != "" && len(value) <= 2048 && strings.TrimSpace(value) == value
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
func warehouseBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WarehouseList] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yanwen", Operation: "listWarehouses", Message: message}
	return sdkgo.NewQueryBranch(branch, WarehouseList{}, &failure, sdkgo.Receipt{})
}
func warehouseRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[WarehouseList] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yanwen", Operation: "listWarehouses", Message: message}
	return sdkgo.NewQueryRetry[WarehouseList](failure, delay)
}
