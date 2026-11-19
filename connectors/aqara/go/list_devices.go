// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package aqara

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListDevicesInput selects one bounded page from the authorized account's devices.
type ListDevicesInput struct {
	// PageNum is a one-based page number; zero selects page one.
	PageNum int `json:"pageNum"`
	// PageSize is a count from 1 to 100; zero selects Aqara's documented default of 50.
	PageSize int `json:"pageSize"`
}

// Device contains non-secret device identity and online state.
type Device struct {
	// DeviceID is Aqara's stable device ID.
	DeviceID string `json:"deviceId"`
	// DeviceName is the user-assigned display name.
	DeviceName string `json:"deviceName"`
	// Model is Aqara's device model identifier.
	Model string `json:"model"`
	// ModelType is 1 or 2 for a gateway and 3 for a child device.
	ModelType int `json:"modelType"`
	// Online reports the documented state value 1.
	Online bool `json:"online"`
	// FirmwareVersion is the installed firmware version if reported.
	FirmwareVersion string `json:"firmwareVersion"`
	// PositionID is the provider location ID if reported.
	PositionID string `json:"positionId"`
	// ParentDeviceID is the gateway device ID for a child device.
	ParentDeviceID string `json:"parentDeviceId"`
	// TimeZone is the device's reported timezone if available.
	TimeZone string `json:"timeZone"`
}

// DevicePage contains one page and the provider's total matching count.
type DevicePage struct {
	// TotalCount is the provider's total device count.
	TotalCount int `json:"totalCount"`
	// PageNum is the one-based page number requested.
	PageNum int `json:"pageNum"`
	// PageSize is the page size requested.
	PageSize int `json:"pageSize"`
	// Devices contains no more than PageSize entries.
	Devices []Device `json:"devices"`
}

// ListDevicesOperation reads one page of authorized devices.
type ListDevicesOperation struct{ client *Client }

// Definition returns the generated list-devices contract.
func (ListDevicesOperation) Definition() sdkgo.QueryDefinition { return ListDevicesDefinition }

// Invoke queries Aqara and classifies provider, protocol, and transport outcomes.
func (operation ListDevicesOperation) Invoke(call sdkgo.Call, input ListDevicesInput) sdkgo.QueryAttempt[DevicePage] {
	pageNum, pageSize := input.PageNum, input.PageSize
	if pageNum == 0 {
		pageNum = 1
	}
	if pageSize == 0 {
		pageSize = 50
	}
	if pageNum < 1 || pageNum > 1000000 || pageSize < 1 || pageSize > 100 {
		return deviceBranch(ListDevicesBranchDefect, sdkgo.FailureValidation, "pageNum or pageSize is outside the supported range")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validCredential(credentials.AppID) || !validCredential(credentials.KeyID) || !validCredential(credentials.AppKey.Reveal()) || !validCredential(credentials.AccessToken.Reveal()) {
		return deviceBranch(ListDevicesBranchDefect, sdkgo.FailureAuthentication, "Aqara connection credentials are unavailable or invalid")
	}
	nonceBytes := make([]byte, 16)
	if _, err = io.ReadFull(operation.client.nonceSource, nonceBytes); err != nil {
		return deviceRetry(sdkgo.FailureAvailability, "secure nonce generation failed", 0)
	}
	nonce := hex.EncodeToString(nonceBytes)
	timestamp := strconv.FormatInt(operation.client.clock().UnixMilli(), 10)
	requestBody, err := json.Marshal(struct {
		Intent string `json:"intent"`
		Data   struct {
			PageNum  int `json:"pageNum"`
			PageSize int `json:"pageSize"`
		} `json:"data"`
	}{Intent: "query.device.info", Data: struct {
		PageNum  int `json:"pageNum"`
		PageSize int `json:"pageSize"`
	}{pageNum, pageSize}})
	if err != nil {
		return deviceBranch(ListDevicesBranchDefect, sdkgo.FailureValidation, "device request cannot be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/v3.0/open/api", bytes.NewReader(requestBody))
	if err != nil {
		return deviceBranch(ListDevicesBranchDefect, sdkgo.FailureValidation, "device request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accesstoken", credentials.AccessToken.Reveal())
	request.Header.Set("Appid", credentials.AppID)
	request.Header.Set("Keyid", credentials.KeyID)
	request.Header.Set("Nonce", nonce)
	request.Header.Set("Time", timestamp)
	request.Header.Set("Sign", signDeviceRequest(credentials.AccessToken.Reveal(), credentials.AppID, credentials.KeyID, nonce, timestamp, credentials.AppKey.Reveal()))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return deviceRetry(sdkgo.FailureAvailability, "Aqara request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return deviceRetry(sdkgo.FailureRateLimit, "Aqara rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return deviceRetry(sdkgo.FailureAvailability, "Aqara is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return deviceBranch(ListDevicesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Aqara response exceeded the configured limit")
	}
	if err != nil {
		return deviceRetry(sdkgo.FailureAvailability, "Aqara response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		return deviceBranch(ListDevicesBranchProviderRejected, classifyDeviceHTTP(response.StatusCode), "Aqara rejected the query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, providerCode, err := decodeDevicePage(body, pageNum, pageSize)
	if err != nil {
		return deviceBranch(ListDevicesBranchInvalidResponse, sdkgo.FailureProtocol, "Aqara returned an invalid device response")
	}
	if providerCode != 0 {
		if providerCode == 429 {
			return deviceRetry(sdkgo.FailureRateLimit, "Aqara rate limited the request", retryAfter)
		}
		if providerCode == 100 || providerCode == 104 || providerCode == 500 || providerCode == 501 {
			return deviceRetry(sdkgo.FailureAvailability, "Aqara is temporarily unavailable", retryAfter)
		}
		return deviceBranch(ListDevicesBranchProviderRejected, classifyDeviceCode(providerCode), "Aqara rejected the query with code "+strconv.Itoa(providerCode))
	}
	return sdkgo.NewQueryBranch(ListDevicesBranchListed, page, nil, sdkgo.Receipt{})
}

func signDeviceRequest(accessToken, appID, keyID, nonce, timestamp, appKey string) string {
	canonical := strings.ToLower("Accesstoken=" + accessToken + "&Appid=" + appID + "&Keyid=" + keyID + "&Nonce=" + nonce + "&Time=" + timestamp + appKey)
	digest := md5.Sum([]byte(canonical))
	return hex.EncodeToString(digest[:])
}

func decodeDevicePage(body []byte, pageNum, pageSize int) (DevicePage, int, error) {
	var wire struct {
		Code   *int `json:"code"`
		Result struct {
			Data []struct {
				DID             string `json:"did"`
				ParentDID       string `json:"parentDid"`
				PositionID      string `json:"positionId"`
				Model           string `json:"model"`
				ModelType       int    `json:"modelType"`
				State           *int   `json:"state"`
				FirmwareVersion string `json:"firmwareVersion"`
				DeviceName      string `json:"deviceName"`
				TimeZone        string `json:"timeZone"`
			} `json:"data"`
			TotalCount int `json:"totalCount"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return DevicePage{}, 0, errors.New("invalid Aqara JSON")
	}
	if *wire.Code != 0 {
		return DevicePage{}, *wire.Code, nil
	}
	if wire.Result.TotalCount < 0 || len(wire.Result.Data) > pageSize || wire.Result.Data == nil {
		return DevicePage{}, 0, errors.New("invalid device count")
	}
	page := DevicePage{TotalCount: wire.Result.TotalCount, PageNum: pageNum, PageSize: pageSize, Devices: make([]Device, 0, len(wire.Result.Data))}
	for _, item := range wire.Result.Data {
		if item.DID == "" || item.Model == "" || item.State == nil || *item.State < 0 || *item.State > 1 {
			return DevicePage{}, 0, fmt.Errorf("invalid device identity or state")
		}
		page.Devices = append(page.Devices, Device{DeviceID: item.DID, DeviceName: item.DeviceName, Model: item.Model, ModelType: item.ModelType, Online: *item.State == 1, FirmwareVersion: item.FirmwareVersion, PositionID: item.PositionID, ParentDeviceID: item.ParentDID, TimeZone: item.TimeZone})
	}
	return page, 0, nil
}

func classifyDeviceCode(code int) sdkgo.FailureKind {
	switch code {
	case 106, 107, 108, 109, 804, 2002, 2004, 2005:
		return sdkgo.FailureAuthentication
	case 403, 603, 706, 717, 755, 756, 803, 2008, 2010, 2013, 2014:
		return sdkgo.FailureAuthorization
	case 101, 102, 103, 105, 302, 303, 304, 305, 306, 604:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

func validCredential(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyDeviceHTTP(status int) sdkgo.FailureKind {
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
func deviceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DevicePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "aqara", Operation: "listDevices", Message: message}
	return sdkgo.NewQueryBranch(branch, DevicePage{}, &failure, sdkgo.Receipt{})
}
func deviceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[DevicePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "aqara", Operation: "listDevices", Message: message}
	return sdkgo.NewQueryRetry[DevicePage](failure, delay)
}
