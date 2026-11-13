// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fiftyonetracking

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetShipmentStatusCountsInput selects the account's aggregate parcel statuses.
type GetShipmentStatusCountsInput struct{}

// ShipmentStatusCounts contains aggregate registered-parcel counts without personal information.
type ShipmentStatusCounts struct {
	// Pending counts parcels awaiting the first lookup.
	Pending int `json:"pending"`
	// NotFound counts parcels without a carrier result.
	NotFound int `json:"notFound"`
	// InTransit counts parcels in transit.
	InTransit int `json:"inTransit"`
	// Pickup counts parcels ready for pickup.
	Pickup int `json:"pickup"`
	// Delivered counts delivered parcels.
	Delivered int `json:"delivered"`
	// Expired counts parcels that exceeded the tracking window.
	Expired int `json:"expired"`
	// Undelivered counts failed delivery attempts.
	Undelivered int `json:"undelivered"`
	// Exception counts parcels with exceptional delivery status.
	Exception int `json:"exception"`
}

// GetShipmentStatusCountsOperation reads aggregate registered-parcel statuses.
type GetShipmentStatusCountsOperation struct{ client *Client }

// Definition returns the generated status-count Query contract.
func (GetShipmentStatusCountsOperation) Definition() sdkgo.QueryDefinition {
	return GetShipmentStatusCountsDefinition
}

// Invoke queries 51Tracking and classifies provider, protocol, and transport outcomes.
func (operation GetShipmentStatusCountsOperation) Invoke(call sdkgo.Call, _ GetShipmentStatusCountsInput) sdkgo.QueryAttempt[ShipmentStatusCounts] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAPIKey(credentials.APIKey.Reveal()) {
		return statusBranch(GetShipmentStatusCountsBranchDefect, sdkgo.FailureAuthentication, "51Tracking API key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v3/trackings/status", nil)
	if err != nil {
		return statusBranch(GetShipmentStatusCountsBranchDefect, sdkgo.FailureValidation, "status-count request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Tracking-Api-Key", credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "51Tracking status-count request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return statusRetry(sdkgo.FailureRateLimit, "51Tracking rate limited status-count query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return statusRetry(sdkgo.FailureAvailability, "51Tracking API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return statusBranch(GetShipmentStatusCountsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "51Tracking response exceeded the configured limit")
	}
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "51Tracking response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return statusBranch(GetShipmentStatusCountsBranchProviderRejected, classifyStatusHTTP(response.StatusCode), "51Tracking rejected status-count query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	counts, isRejected, err := decodeStatusCounts(body)
	if err != nil {
		return statusBranch(GetShipmentStatusCountsBranchInvalidResponse, sdkgo.FailureProtocol, "51Tracking returned an invalid status-count response")
	}
	if isRejected {
		return statusBranch(GetShipmentStatusCountsBranchProviderRejected, sdkgo.FailureProviderRejection, "51Tracking rejected status-count query")
	}
	return sdkgo.NewQueryBranch(GetShipmentStatusCountsBranchCounted, counts, nil, sdkgo.Receipt{})
}

func decodeStatusCounts(body []byte) (ShipmentStatusCounts, bool, error) {
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			Pending     *int `json:"pending"`
			NotFound    *int `json:"notfound"`
			InTransit   *int `json:"transit"`
			Pickup      *int `json:"pickup"`
			Delivered   *int `json:"delivered"`
			Expired     *int `json:"expired"`
			Undelivered *int `json:"undelivered"`
			Exception   *int `json:"exception"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return ShipmentStatusCounts{}, false, errors.New("invalid 51Tracking JSON")
	}
	if *wire.Code != 200 {
		return ShipmentStatusCounts{}, true, nil
	}
	if wire.Data == nil || wire.Data.Pending == nil || wire.Data.NotFound == nil || wire.Data.InTransit == nil || wire.Data.Pickup == nil || wire.Data.Delivered == nil || wire.Data.Expired == nil || wire.Data.Undelivered == nil || wire.Data.Exception == nil {
		return ShipmentStatusCounts{}, false, errors.New("missing 51Tracking status counts")
	}
	counts := ShipmentStatusCounts{Pending: *wire.Data.Pending, NotFound: *wire.Data.NotFound, InTransit: *wire.Data.InTransit, Pickup: *wire.Data.Pickup, Delivered: *wire.Data.Delivered, Expired: *wire.Data.Expired, Undelivered: *wire.Data.Undelivered, Exception: *wire.Data.Exception}
	for _, value := range []int{counts.Pending, counts.NotFound, counts.InTransit, counts.Pickup, counts.Delivered, counts.Expired, counts.Undelivered, counts.Exception} {
		if value < 0 {
			return ShipmentStatusCounts{}, false, errors.New("negative 51Tracking status count")
		}
	}
	return counts, false, nil
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
func classifyStatusHTTP(status int) sdkgo.FailureKind {
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
func statusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ShipmentStatusCounts] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fiftyonetracking", Operation: "getShipmentStatusCounts", Message: message}
	return sdkgo.NewQueryBranch(branch, ShipmentStatusCounts{}, &failure, sdkgo.Receipt{})
}
func statusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ShipmentStatusCounts] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fiftyonetracking", Operation: "getShipmentStatusCounts", Message: message}
	return sdkgo.NewQueryRetry[ShipmentStatusCounts](failure, delay)
}
