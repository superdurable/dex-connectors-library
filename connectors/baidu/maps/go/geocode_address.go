// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package baidumaps

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GeocodeAddressInput selects a structured Chinese address and optional city.
type GeocodeAddressInput struct {
	// Address is a UTF-8 address of at most 128 bytes, from province to street number.
	Address string `json:"address"`
	// City is an optional Chinese city name used to help distinguish repeated addresses.
	City string `json:"city,omitempty"`
}

// GeocodeAddressOutput is Baidu Maps' best matching BD-09 location and precision measures.
type GeocodeAddressOutput struct {
	// Longitude is the BD-09 east-west coordinate in degrees.
	Longitude float64 `json:"longitude"`
	// Latitude is the BD-09 north-south coordinate in degrees.
	Latitude float64 `json:"latitude"`
	// Precise reports whether Baidu Maps found an exact point.
	Precise bool `json:"precise"`
	// Confidence is the provider's 0 through 100 absolute precision score.
	Confidence int `json:"confidence"`
	// Comprehension is the provider's 0 through 100 address understanding score.
	Comprehension int `json:"comprehension"`
	// Level is the provider's match structure, such as a road or door address.
	Level string `json:"level"`
}

// GeocodeAddressOperation reads one address lookup.
type GeocodeAddressOperation struct{ client *Client }

// Definition returns the generated geocoding contract.
func (GeocodeAddressOperation) Definition() sdkgo.QueryDefinition { return GeocodeAddressDefinition }

// Invoke resolves the address and classifies provider and transport outcomes.
func (operation GeocodeAddressOperation) Invoke(call sdkgo.Call, input GeocodeAddressInput) sdkgo.QueryAttempt[GeocodeAddressOutput] {
	if strings.TrimSpace(input.Address) == "" || len(input.Address) > 128 || len(input.City) > 128 {
		return geocodeBranch(GeocodeAddressBranchDefect, sdkgo.FailureValidation, "address or city is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccessKey(credentials.AccessKey.Reveal()) {
		return geocodeBranch(GeocodeAddressBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.geocodeURL(credentials.AccessKey.Reveal(), input), nil)
	if err != nil {
		return geocodeBranch(GeocodeAddressBranchDefect, sdkgo.FailureValidation, "geocoding request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return geocodeRetry(sdkgo.FailureAvailability, "Baidu Maps request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return geocodeRetry(sdkgo.FailureRateLimit, "Baidu Maps rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return geocodeRetry(sdkgo.FailureAvailability, "Baidu Maps is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return geocodeBranch(GeocodeAddressBranchProviderRejected, sdkgo.FailureProviderRejection, "Baidu Maps rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return geocodeBranch(GeocodeAddressBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Baidu Maps response exceeded the configured limit")
	}
	if err != nil {
		return geocodeRetry(sdkgo.FailureAvailability, "Baidu Maps response was interrupted", 0)
	}
	output, providerStatus, err := decodeGeocodeResponse(body)
	if err != nil {
		return geocodeBranch(GeocodeAddressBranchInvalidResponse, sdkgo.FailureProtocol, "Baidu Maps returned an invalid response")
	}
	if providerStatus == 1 {
		return geocodeRetry(sdkgo.FailureAvailability, "Baidu Maps reported a server error", retryAfter)
	}
	if providerStatus != 0 {
		return geocodeBranch(GeocodeAddressBranchProviderRejected, classifyProviderStatus(providerStatus), "Baidu Maps rejected the geocoding request")
	}
	return sdkgo.NewQueryBranch(GeocodeAddressBranchGeocoded, output, nil, sdkgo.Receipt{})
}

func decodeGeocodeResponse(body []byte) (GeocodeAddressOutput, int, error) {
	var wire struct {
		Status *int `json:"status"`
		Result *struct {
			Location *struct {
				Longitude *float64 `json:"lng"`
				Latitude  *float64 `json:"lat"`
			} `json:"location"`
			Precise       *int   `json:"precise"`
			Confidence    *int   `json:"confidence"`
			Comprehension *int   `json:"comprehension"`
			Level         string `json:"level"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Status == nil {
		return GeocodeAddressOutput{}, 0, errors.New("invalid Baidu Maps envelope")
	}
	if *wire.Status != 0 {
		return GeocodeAddressOutput{}, *wire.Status, nil
	}
	if wire.Result == nil || wire.Result.Location == nil || wire.Result.Location.Longitude == nil || wire.Result.Location.Latitude == nil ||
		wire.Result.Precise == nil || wire.Result.Confidence == nil || wire.Result.Comprehension == nil {
		return GeocodeAddressOutput{}, 0, errors.New("incomplete Baidu Maps result")
	}
	longitude, latitude := *wire.Result.Location.Longitude, *wire.Result.Location.Latitude
	if math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 ||
		math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 ||
		(*wire.Result.Precise != 0 && *wire.Result.Precise != 1) ||
		*wire.Result.Confidence < 0 || *wire.Result.Confidence > 100 ||
		*wire.Result.Comprehension < 0 || *wire.Result.Comprehension > 100 {
		return GeocodeAddressOutput{}, 0, errors.New("invalid Baidu Maps result values")
	}
	return GeocodeAddressOutput{Longitude: longitude, Latitude: latitude, Precise: *wire.Result.Precise == 1,
		Confidence: *wire.Result.Confidence, Comprehension: *wire.Result.Comprehension, Level: wire.Result.Level}, 0, nil
}

func classifyProviderStatus(status int) sdkgo.FailureKind {
	switch status {
	case 2:
		return sdkgo.FailureValidation
	case 3, 102, 240:
		return sdkgo.FailureAuthorization
	case 4:
		return sdkgo.FailureQuotaExhausted
	case 5, 101:
		return sdkgo.FailureAuthentication
	default:
		if status >= 200 && status < 300 {
			return sdkgo.FailureAuthorization
		}
		if status >= 300 && status < 400 {
			return sdkgo.FailureQuotaExhausted
		}
		return sdkgo.FailureProviderRejection
	}
}

func geocodeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GeocodeAddressOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "baidu-maps", Operation: "geocodeAddress", Message: message}
	return sdkgo.NewQueryBranch(branch, GeocodeAddressOutput{}, &failure, sdkgo.Receipt{})
}

func geocodeRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GeocodeAddressOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "baidu-maps", Operation: "geocodeAddress", Message: message}
	return sdkgo.NewQueryRetry[GeocodeAddressOutput](failure, delay)
}
