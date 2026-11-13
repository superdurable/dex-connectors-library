// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onenet

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// QueryDeviceDetailInput selects one device by its name in the configured product.
type QueryDeviceDetailInput struct {
	// DeviceName is the device name shown in OneNET Product Development > Device Management.
	DeviceName string `json:"deviceName"`
}

// DeviceDetail contains nonsecret OneNET device metadata.
type DeviceDetail struct {
	// ProductID identifies the configured product.
	ProductID string `json:"productId"`
	// DeviceName identifies the device within that product.
	DeviceName string `json:"deviceName"`
	// Status is 0 offline, 1 online, or 2 not activated.
	Status int `json:"status"`
	// Description is the optional device description.
	Description string `json:"description"`
	// CreateTime is the provider's device creation timestamp.
	CreateTime string `json:"createTime"`
	// LastOnlineTime is the provider's last online timestamp.
	LastOnlineTime string `json:"lastOnlineTime"`
}

// QueryDeviceDetailOperation reads one device without retaining provider secrets.
type QueryDeviceDetailOperation struct{ client *Client }

// QueryDeviceDetail binds the Query to this client.
func (client *Client) QueryDeviceDetail() QueryDeviceDetailOperation {
	return QueryDeviceDetailOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (QueryDeviceDetailOperation) Definition() sdkgo.QueryDefinition {
	return QueryDeviceDetailDefinition
}

// Invoke queries OneNET and classifies the provider outcome.
func (operation QueryDeviceDetailOperation) Invoke(call sdkgo.Call, input QueryDeviceDetailInput) sdkgo.QueryAttempt[DeviceDetail] {
	response := operation.client.queryDevice(call, input)
	if errors.Is(response.err, errCredential) {
		return deviceBranch(QueryDeviceDetailBranchDefect, sdkgo.FailureAuthentication, "OneNET credentials or device name are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[DeviceDetail](deviceFailure(sdkgo.FailureTransport, "OneNET request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[DeviceDetail](deviceFailure(sdkgo.FailureRateLimit, "OneNET rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[DeviceDetail](deviceFailure(sdkgo.FailureAvailability, "OneNET is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return deviceBranch(QueryDeviceDetailBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "OneNET response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[DeviceDetail](deviceFailure(sdkgo.FailureTransport, "OneNET response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return deviceBranch(QueryDeviceDetailBranchProviderRejected, sdkgo.FailureProviderRejection, "OneNET rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return deviceBranch(QueryDeviceDetailBranchInvalidResponse, sdkgo.FailureProtocol, "OneNET returned invalid UTF-8")
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			ProductID      string `json:"pid"`
			DeviceName     string `json:"name"`
			Status         *int   `json:"status"`
			Description    string `json:"desc"`
			CreateTime     string `json:"create_time"`
			LastOnlineTime string `json:"last_time"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return deviceBranch(QueryDeviceDetailBranchInvalidResponse, sdkgo.FailureProtocol, "OneNET returned invalid device JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return deviceBranch(QueryDeviceDetailBranchInvalidResponse, sdkgo.FailureProtocol, "OneNET returned trailing JSON")
	}
	if wire.Code == nil {
		return deviceBranch(QueryDeviceDetailBranchInvalidResponse, sdkgo.FailureProtocol, "OneNET omitted response code")
	}
	if *wire.Code != 0 {
		return deviceBranch(QueryDeviceDetailBranchProviderRejected, sdkgo.FailureProviderRejection, "OneNET rejected code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.ProductID == "" || wire.Data.DeviceName == "" || wire.Data.Status == nil || *wire.Data.Status < 0 || *wire.Data.Status > 2 {
		return deviceBranch(QueryDeviceDetailBranchInvalidResponse, sdkgo.FailureProtocol, "OneNET returned incomplete device detail")
	}
	output := DeviceDetail{ProductID: wire.Data.ProductID, DeviceName: wire.Data.DeviceName, Status: *wire.Data.Status, Description: wire.Data.Description, CreateTime: wire.Data.CreateTime, LastOnlineTime: wire.Data.LastOnlineTime}
	return sdkgo.NewQueryBranch(QueryDeviceDetailBranchFound, output, nil, sdkgo.Receipt{})
}
func deviceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "onenet", Operation: "queryDeviceDetail", Message: message}
}
func deviceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DeviceDetail] {
	failure := deviceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, DeviceDetail{}, &failure, sdkgo.Receipt{})
}
