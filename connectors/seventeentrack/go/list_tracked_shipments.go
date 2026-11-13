// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package seventeentrack

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

// ListTrackedShipmentsInput selects a 40-record page of already registered shipments.
type ListTrackedShipmentsInput struct {
	// PageNumber is one-based; zero selects the first page.
	PageNumber int `json:"pageNumber"`
}

// TrackedShipment contains only a registered number and tracking statuses.
type TrackedShipment struct {
	// Number is the registered tracking number.
	Number string `json:"number"`
	// Carrier is the 17TRACK carrier code.
	Carrier int `json:"carrier"`
	// PackageStatus is the latest package status.
	PackageStatus string `json:"packageStatus"`
	// TrackingStatus says whether tracking is active or stopped.
	TrackingStatus string `json:"trackingStatus"`
}

// TrackedShipmentPage contains a bounded tracking-list page and pagination metadata.
type TrackedShipmentPage struct {
	// PageNumber is the provider's one-based page number.
	PageNumber int `json:"pageNumber"`
	// Total is the provider's count of matching shipments.
	Total int `json:"total"`
	// Shipments contains at most 40 registered shipments.
	Shipments []TrackedShipment `json:"shipments"`
}

// ListTrackedShipmentsOperation reads already registered tracking statuses.
type ListTrackedShipmentsOperation struct{ client *Client }

// Definition returns the generated tracking-list Query contract.
func (ListTrackedShipmentsOperation) Definition() sdkgo.QueryDefinition {
	return ListTrackedShipmentsDefinition
}

// Invoke queries 17TRACK and classifies provider, protocol, and transport outcomes.
func (operation ListTrackedShipmentsOperation) Invoke(call sdkgo.Call, input ListTrackedShipmentsInput) sdkgo.QueryAttempt[TrackedShipmentPage] {
	if input.PageNumber < 0 || input.PageNumber > 100000 {
		return shipmentBranch(ListTrackedShipmentsBranchDefect, sdkgo.FailureValidation, "17TRACK page number must be between 1 and 100000")
	}
	pageNumber := input.PageNumber
	if pageNumber == 0 {
		pageNumber = 1
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAPIKey(credentials.APIKey.Reveal()) {
		return shipmentBranch(ListTrackedShipmentsBranchDefect, sdkgo.FailureAuthentication, "17TRACK API key is unavailable or invalid")
	}
	requestBody, err := json.Marshal(map[string]int{"page_no": pageNumber})
	if err != nil {
		return shipmentBranch(ListTrackedShipmentsBranchDefect, sdkgo.FailureValidation, "tracking-list request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/track/v2.4/gettracklist", bytes.NewReader(requestBody))
	if err != nil {
		return shipmentBranch(ListTrackedShipmentsBranchDefect, sdkgo.FailureValidation, "tracking-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("17token", credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return shipmentRetry(sdkgo.FailureAvailability, "17TRACK tracking-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return shipmentRetry(sdkgo.FailureRateLimit, "17TRACK rate limited tracking-list query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return shipmentRetry(sdkgo.FailureAvailability, "17TRACK tracking API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return shipmentBranch(ListTrackedShipmentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "17TRACK response exceeded the configured limit")
	}
	if err != nil {
		return shipmentRetry(sdkgo.FailureAvailability, "17TRACK response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return shipmentBranch(ListTrackedShipmentsBranchProviderRejected, classifyShipmentHTTP(response.StatusCode), "17TRACK rejected tracking-list query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, isRejected, err := decodeShipmentPage(body)
	if err != nil {
		return shipmentBranch(ListTrackedShipmentsBranchInvalidResponse, sdkgo.FailureProtocol, "17TRACK returned an invalid tracking-list response")
	}
	if isRejected {
		return shipmentBranch(ListTrackedShipmentsBranchProviderRejected, sdkgo.FailureProviderRejection, "17TRACK rejected tracking-list query")
	}
	return sdkgo.NewQueryBranch(ListTrackedShipmentsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeShipmentPage(body []byte) (TrackedShipmentPage, bool, error) {
	var wire struct {
		Code *int `json:"code"`
		Page *struct {
			Total  int `json:"data_total"`
			Number int `json:"page_no"`
			Size   int `json:"page_size"`
		} `json:"page"`
		Data *struct {
			Accepted []struct {
				Number         string `json:"number"`
				Carrier        int    `json:"carrier"`
				PackageStatus  string `json:"package_status"`
				TrackingStatus string `json:"tracking_status"`
			} `json:"accepted"`
			Errors []json.RawMessage `json:"errors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return TrackedShipmentPage{}, false, errors.New("invalid 17TRACK JSON")
	}
	if *wire.Code != 0 {
		return TrackedShipmentPage{}, true, nil
	}
	if wire.Data == nil {
		return TrackedShipmentPage{}, false, errors.New("missing 17TRACK data")
	}
	if len(wire.Data.Errors) > 0 {
		return TrackedShipmentPage{}, true, nil
	}
	if wire.Page == nil || wire.Page.Number < 0 || wire.Page.Size > 40 || wire.Page.Total < 0 || wire.Data.Accepted == nil || len(wire.Data.Accepted) > 40 {
		return TrackedShipmentPage{}, false, errors.New("invalid 17TRACK page")
	}
	page := TrackedShipmentPage{PageNumber: wire.Page.Number, Total: wire.Page.Total, Shipments: make([]TrackedShipment, 0, len(wire.Data.Accepted))}
	for _, item := range wire.Data.Accepted {
		if item.Number == "" || item.Carrier <= 0 {
			return TrackedShipmentPage{}, false, errors.New("invalid 17TRACK shipment")
		}
		page.Shipments = append(page.Shipments, TrackedShipment{Number: item.Number, Carrier: item.Carrier, PackageStatus: item.PackageStatus, TrackingStatus: item.TrackingStatus})
	}
	return page, false, nil
}

func validAPIKey(key string) bool {
	if key == "" || len(key) > 2048 || strings.TrimSpace(key) != key {
		return false
	}
	for _, character := range key {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyShipmentHTTP(status int) sdkgo.FailureKind {
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
func shipmentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TrackedShipmentPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "seventeentrack", Operation: "listTrackedShipments", Message: message}
	return sdkgo.NewQueryBranch(branch, TrackedShipmentPage{}, &failure, sdkgo.Receipt{})
}
func shipmentRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[TrackedShipmentPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "seventeentrack", Operation: "listTrackedShipments", Message: message}
	return sdkgo.NewQueryRetry[TrackedShipmentPage](failure, delay)
}
