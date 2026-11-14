// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package seniverse

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetCurrentWeatherInput selects a city ID, city name, or other documented Seniverse location.
type GetCurrentWeatherInput struct {
	// Location is a Seniverse city ID, Chinese city name, pinyin name, or latitude:longitude pair.
	Location string `json:"location"`
}

// GetCurrentWeatherOutput contains fields available to free and paid Seniverse products.
type GetCurrentWeatherOutput struct {
	// LocationID is Seniverse's city ID.
	LocationID string `json:"locationId"`
	// LocationName is Seniverse's localized city name.
	LocationName string `json:"locationName"`
	// Country is the two-letter country code.
	Country string `json:"country"`
	// WeatherText is the localized current weather condition.
	WeatherText string `json:"weatherText"`
	// WeatherCode is Seniverse's weather condition code.
	WeatherCode string `json:"weatherCode"`
	// Temperature is the provider's numeric temperature string.
	Temperature string `json:"temperature"`
	// TemperatureUnit is c for Celsius or f for Fahrenheit.
	TemperatureUnit string `json:"temperatureUnit"`
	// LastUpdate is the provider's RFC3339 local observation time.
	LastUpdate string `json:"lastUpdate"`
}

// GetCurrentWeatherOperation reads one current-weather observation.
type GetCurrentWeatherOperation struct{ client *Client }

// Definition returns the generated current-weather contract.
func (GetCurrentWeatherOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrentWeatherDefinition
}

// Invoke looks up the weather and classifies provider and transport outcomes.
func (operation GetCurrentWeatherOperation) Invoke(call sdkgo.Call, input GetCurrentWeatherInput) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	if strings.TrimSpace(input.Location) == "" || strings.TrimSpace(input.Location) != input.Location || len(input.Location) > 256 {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "location is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidPrivateKey(credentials.PrivateKey.Reveal()) {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	// Seniverse key mode requires key as a query parameter: https://docs.seniverse.com/api/start/validation.html
	query := url.Values{"key": {credentials.PrivateKey.Reveal()}, "location": {input.Location}, "language": {operation.client.language}, "unit": {operation.client.unit}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v3/weather/now.json?"+query.Encode(), nil)
	if err != nil {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "weather request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Seniverse request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return currentWeatherRetry(sdkgo.FailureRateLimit, "Seniverse rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Seniverse is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Seniverse response exceeded the configured limit")
	}
	if err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Seniverse response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		var providerError struct {
			StatusCode string `json:"status_code"`
		}
		if json.Unmarshal(body, &providerError) == nil && providerError.StatusCode == "AP010014" {
			return currentWeatherRetry(sdkgo.FailureRateLimit, "Seniverse rate limited the request", retryAfter)
		}
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, classifySeniverseError(providerError.StatusCode), "Seniverse rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	output, err := decodeCurrentWeatherResponse(body, operation.client.unit)
	if err != nil {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "Seniverse returned an invalid response")
	}
	return sdkgo.NewQueryBranch(GetCurrentWeatherBranchFetched, output, nil, sdkgo.Receipt{})
}

func decodeCurrentWeatherResponse(body []byte, unit string) (GetCurrentWeatherOutput, error) {
	var wire struct {
		Results []struct {
			Location struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Country string `json:"country"`
			} `json:"location"`
			Now struct {
				Text        string `json:"text"`
				Code        string `json:"code"`
				Temperature string `json:"temperature"`
			} `json:"now"`
			LastUpdate string `json:"last_update"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || len(wire.Results) != 1 {
		return GetCurrentWeatherOutput{}, errors.New("invalid weather result count")
	}
	result := wire.Results[0]
	if result.Location.ID == "" || result.Location.Name == "" || result.Now.Text == "" || result.Now.Code == "" || result.Now.Temperature == "" || result.LastUpdate == "" {
		return GetCurrentWeatherOutput{}, errors.New("missing weather field")
	}
	temperature, err := strconv.ParseFloat(result.Now.Temperature, 64)
	if err != nil || math.IsNaN(temperature) || math.IsInf(temperature, 0) {
		return GetCurrentWeatherOutput{}, errors.New("invalid temperature")
	}
	if _, err := time.Parse(time.RFC3339, result.LastUpdate); err != nil {
		return GetCurrentWeatherOutput{}, errors.New("invalid update time")
	}
	return GetCurrentWeatherOutput{LocationID: result.Location.ID, LocationName: result.Location.Name, Country: result.Location.Country, WeatherText: result.Now.Text, WeatherCode: result.Now.Code, Temperature: result.Now.Temperature, TemperatureUnit: unit, LastUpdate: result.LastUpdate}, nil
}

func isValidPrivateKey(privateKey string) bool {
	if privateKey == "" || len(privateKey) > 512 || strings.TrimSpace(privateKey) != privateKey {
		return false
	}
	for _, character := range privateKey {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}

func classifySeniverseError(statusCode string) sdkgo.FailureKind {
	switch statusCode {
	case "AP010003", "AP010004":
		return sdkgo.FailureAuthentication
	case "AP010002", "AP010006", "AP010008", "AP010009":
		return sdkgo.FailureAuthorization
	case "AP010012", "AP010013":
		return sdkgo.FailureQuotaExhausted
	case "AP010001", "AP010010", "AP010011", "AP010017":
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

func currentWeatherBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "seniverse", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryBranch(branch, GetCurrentWeatherOutput{}, &failure, sdkgo.Receipt{})
}
func currentWeatherRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "seniverse", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryRetry[GetCurrentWeatherOutput](failure, delay)
}
