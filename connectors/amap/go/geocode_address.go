// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amap

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

// GeocodeAddressInput selects a structured address and optional city scope.
type GeocodeAddressInput struct {
	// Address is the Chinese address, from province to street number.
	Address string `json:"address"`
	// City optionally limits matching to a Chinese city name, pinyin, citycode, or adcode.
	City string `json:"city,omitempty"`
}

// GeocodedLocation is one location candidate returned by Amap.
type GeocodedLocation struct {
	// FormattedAddress is Amap's complete address.
	FormattedAddress string `json:"formattedAddress"`
	// Province is the province or direct-administered municipality.
	Province string `json:"province"`
	// City is the city when Amap supplies one.
	City string `json:"city"`
	// District is the district when Amap supplies one.
	District string `json:"district"`
	// Adcode is Amap's administrative code.
	Adcode string `json:"adcode"`
	// Longitude is the first coordinate in Amap's location pair.
	Longitude float64 `json:"longitude"`
	// Latitude is the second coordinate in Amap's location pair.
	Latitude float64 `json:"latitude"`
	// Level is Amap's match-precision label.
	Level string `json:"level"`
}

// GeocodeAddressOutput contains zero or more provider location candidates.
type GeocodeAddressOutput struct {
	// Locations is empty when Amap found no match.
	Locations []GeocodedLocation `json:"locations"`
}

// GeocodeAddressOperation reads one address lookup.
type GeocodeAddressOperation struct{ client *Client }

// Definition returns the generated geocoding contract.
func (GeocodeAddressOperation) Definition() sdkgo.QueryDefinition { return GeocodeAddressDefinition }

// Invoke looks up the address and classifies provider and transport outcomes.
func (operation GeocodeAddressOperation) Invoke(call sdkgo.Call, input GeocodeAddressInput) sdkgo.QueryAttempt[GeocodeAddressOutput] {
	if strings.TrimSpace(input.Address) == "" || len(input.Address) > 512 || len(input.City) > 128 {
		return geocodeBranch(GeocodeAddressBranchDefect, sdkgo.FailureValidation, "address or city is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return geocodeBranch(GeocodeAddressBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.geocodeURL(credentials.APIKey.Reveal(), input), nil)
	if err != nil {
		return geocodeBranch(GeocodeAddressBranchDefect, sdkgo.FailureValidation, "geocoding request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return geocodeRetry(sdkgo.FailureAvailability, "Amap request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return geocodeRetry(sdkgo.FailureRateLimit, "Amap rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return geocodeRetry(sdkgo.FailureAvailability, "Amap is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return geocodeBranch(GeocodeAddressBranchProviderRejected, sdkgo.FailureProviderRejection, "Amap rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return geocodeBranch(GeocodeAddressBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Amap response exceeded the configured limit")
	}
	if err != nil {
		return geocodeRetry(sdkgo.FailureAvailability, "Amap response was interrupted", 0)
	}
	output, infocode, err := decodeGeocodeResponse(body)
	if err != nil {
		return geocodeBranch(GeocodeAddressBranchInvalidResponse, sdkgo.FailureProtocol, "Amap returned an invalid response")
	}
	if infocode == "10004" || infocode == "10014" {
		return geocodeRetry(sdkgo.FailureRateLimit, "Amap rate limited the request", retryAfter)
	}
	if infocode != "10000" {
		return geocodeBranch(GeocodeAddressBranchProviderRejected, classifyAmapFailure(infocode), "Amap rejected the geocoding request")
	}
	return sdkgo.NewQueryBranch(GeocodeAddressBranchGeocoded, output, nil, sdkgo.Receipt{})
}

func decodeGeocodeResponse(body []byte) (GeocodeAddressOutput, string, error) {
	var wire struct {
		Status   string `json:"status"`
		Infocode string `json:"infocode"`
		Count    string `json:"count"`
		Geocodes []struct {
			FormattedAddress string          `json:"formatted_address"`
			Province         json.RawMessage `json:"province"`
			City             json.RawMessage `json:"city"`
			District         json.RawMessage `json:"district"`
			Adcode           string          `json:"adcode"`
			Location         string          `json:"location"`
			Level            string          `json:"level"`
		} `json:"geocodes"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || (wire.Status != "0" && wire.Status != "1") || wire.Infocode == "" {
		return GeocodeAddressOutput{}, "", errors.New("invalid Amap envelope")
	}
	if wire.Status == "0" {
		return GeocodeAddressOutput{}, wire.Infocode, nil
	}
	if wire.Infocode != "10000" || wire.Geocodes == nil {
		return GeocodeAddressOutput{}, "", errors.New("invalid Amap success")
	}
	count, err := strconv.Atoi(wire.Count)
	if err != nil || count != len(wire.Geocodes) || count > 100 {
		return GeocodeAddressOutput{}, "", errors.New("invalid Amap count")
	}
	output := GeocodeAddressOutput{Locations: make([]GeocodedLocation, 0, count)}
	for _, item := range wire.Geocodes {
		longitude, latitude, err := parseLocation(item.Location)
		if err != nil {
			return GeocodeAddressOutput{}, "", err
		}
		output.Locations = append(output.Locations, GeocodedLocation{FormattedAddress: item.FormattedAddress,
			Province: stringOrEmpty(item.Province), City: stringOrEmpty(item.City), District: stringOrEmpty(item.District),
			Adcode: item.Adcode, Longitude: longitude, Latitude: latitude, Level: item.Level})
	}
	return output, wire.Infocode, nil
}

func parseLocation(location string) (float64, float64, error) {
	parts := strings.Split(location, ",")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid Amap location")
	}
	longitude, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		return 0, 0, errors.New("invalid Amap longitude")
	}
	latitude, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 {
		return 0, 0, errors.New("invalid Amap latitude")
	}
	return longitude, latitude, nil
}

func stringOrEmpty(value json.RawMessage) string {
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		return ""
	}
	return result
}

func classifyAmapFailure(infocode string) sdkgo.FailureKind {
	switch infocode {
	case "10001", "10013":
		return sdkgo.FailureAuthentication
	case "10002", "10005", "10007", "10009", "10012":
		return sdkgo.FailureAuthorization
	case "10003", "10010":
		return sdkgo.FailureQuotaExhausted
	case "20000", "20001":
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

func geocodeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GeocodeAddressOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "amap", Operation: "geocodeAddress", Message: message}
	return sdkgo.NewQueryBranch(branch, GeocodeAddressOutput{Locations: []GeocodedLocation{}}, &failure, sdkgo.Receipt{})
}

func geocodeRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GeocodeAddressOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "amap", Operation: "geocodeAddress", Message: message}
	return sdkgo.NewQueryRetry[GeocodeAddressOutput](failure, delay)
}
