// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package winit

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ListWarehousesInput selects all warehouses visible to the authorized seller account.
type ListWarehousesInput struct{}

// Warehouse identifies an overseas fulfillment warehouse without its street address.
type Warehouse struct {
	// ID is the provider's numeric warehouse ID.
	ID int64 `json:"id"`
	// Code is the provider's warehouse code for order creation.
	Code string `json:"code"`
	// Name is the provider's warehouse display name.
	Name string `json:"name"`
}

// WarehousePage contains the bounded authorized warehouse catalog.
type WarehousePage struct {
	// Warehouses contains at most 200 records.
	Warehouses []Warehouse `json:"warehouses"`
}

// ListWarehousesOperation reads warehouse IDs and codes for the seller account.
type ListWarehousesOperation struct{ client *Client }

// Definition returns the generated warehouse-list contract.
func (ListWarehousesOperation) Definition() sdkgo.QueryDefinition { return ListWarehousesDefinition }

// Invoke queries Winit and classifies provider, protocol, and transport outcomes.
func (operation ListWarehousesOperation) Invoke(call sdkgo.Call, _ ListWarehousesInput) sdkgo.QueryAttempt[WarehousePage] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validCredential(credentials.AppKey.Reveal()) || !validCredential(credentials.Token.Reveal()) || !validCredential(credentials.ClientID.Reveal()) || !validCredential(credentials.ClientSecret.Reveal()) {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureAuthentication, "Winit seller or application credentials are unavailable or invalid")
	}
	timestamp := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02 15:04:05")
	parameters := map[string]string{"action": "queryWarehouse", "app_key": credentials.AppKey.Reveal(), "format": "json", "platform": operation.client.platform, "sign_method": "md5", "timestamp": timestamp, "version": "1.0"}
	requestBody := struct {
		Action     string `json:"action"`
		AppKey     string `json:"app_key"`
		ClientID   string `json:"client_id"`
		ClientSign string `json:"client_sign"`
		Format     string `json:"format"`
		Platform   string `json:"platform"`
		Sign       string `json:"sign"`
		SignMethod string `json:"sign_method"`
		Timestamp  string `json:"timestamp"`
		Version    string `json:"version"`
	}{Action: parameters["action"], AppKey: parameters["app_key"], ClientID: credentials.ClientID.Reveal(), ClientSign: signWinit(parameters, credentials.ClientSecret.Reveal()), Format: parameters["format"], Platform: parameters["platform"], Sign: signWinit(parameters, credentials.Token.Reveal()), SignMethod: parameters["sign_method"], Timestamp: timestamp, Version: parameters["version"]}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureValidation, "Winit warehouse request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/openapi/service", bytes.NewReader(body))
	if err != nil {
		return warehouseBranch(ListWarehousesBranchDefect, sdkgo.FailureValidation, "Winit warehouse request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return warehouseRetry(sdkgo.FailureAvailability, "Winit warehouse request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return warehouseRetry(sdkgo.FailureRateLimit, "Winit rate limited warehouse query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return warehouseRetry(sdkgo.FailureAvailability, "Winit warehouse API is temporarily unavailable", retryAfter)
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return warehouseBranch(ListWarehousesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Winit response exceeded the configured limit")
	}
	if err != nil {
		return warehouseRetry(sdkgo.FailureAvailability, "Winit response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return warehouseBranch(ListWarehousesBranchProviderRejected, classifyWarehouseHTTP(response.StatusCode), "Winit rejected warehouse query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, code, err := decodeWarehousePage(responseBody)
	if err != nil {
		return warehouseBranch(ListWarehousesBranchInvalidResponse, sdkgo.FailureProtocol, "Winit returned an invalid warehouse response")
	}
	if code != 0 {
		return warehouseBranch(ListWarehousesBranchProviderRejected, sdkgo.FailureProviderRejection, "Winit rejected warehouse query with code "+strconv.Itoa(code))
	}
	return sdkgo.NewQueryBranch(ListWarehousesBranchListed, page, nil, sdkgo.Receipt{})
}
func signWinit(parameters map[string]string, secret string) string {
	keys := make([]string, 0, len(parameters))
	for key, value := range parameters {
		if value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var message strings.Builder
	message.WriteString(secret)
	for _, key := range keys {
		message.WriteString(key)
		message.WriteString(parameters[key])
	}
	message.WriteString(secret)
	digest := md5.Sum([]byte(message.String()))
	return strings.ToUpper(hex.EncodeToString(digest[:]))
}
func decodeWarehousePage(body []byte) (WarehousePage, int, error) {
	var wire struct {
		Code *int `json:"code"`
		Data []struct {
			ID   *int64 `json:"warehouseID"`
			Code string `json:"warehouseCode"`
			Name string `json:"warehouseName"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return WarehousePage{}, 0, errors.New("invalid Winit JSON")
	}
	if *wire.Code != 0 {
		return WarehousePage{}, *wire.Code, nil
	}
	if wire.Data == nil || len(wire.Data) > 200 {
		return WarehousePage{}, 0, errors.New("invalid Winit warehouse catalog")
	}
	page := WarehousePage{Warehouses: make([]Warehouse, 0, len(wire.Data))}
	for _, item := range wire.Data {
		if item.ID == nil || *item.ID <= 0 || item.Code == "" || item.Name == "" {
			return WarehousePage{}, 0, errors.New("invalid Winit warehouse")
		}
		page.Warehouses = append(page.Warehouses, Warehouse{ID: *item.ID, Code: item.Code, Name: item.Name})
	}
	return page, 0, nil
}
func validPlatform(platform string) bool {
	if len(platform) < 1 || len(platform) > 128 {
		return false
	}
	for _, character := range platform {
		if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}
func validCredential(value string) bool {
	if value == "" || len(value) > 2048 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
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
	case 400, 404:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func warehouseBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WarehousePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "winit", Operation: "listWarehouses", Message: message}
	return sdkgo.NewQueryBranch(branch, WarehousePage{}, &failure, sdkgo.Receipt{})
}
func warehouseRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[WarehousePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "winit", Operation: "listWarehouses", Message: message}
	return sdkgo.NewQueryRetry[WarehousePage](failure, delay)
}
