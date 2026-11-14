// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package juhe

import (
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

// GetCurrentWeatherInput identifies one supported Chinese city by name or Juhe city ID.
type GetCurrentWeatherInput struct {
	// City is the exact city name or ID from Juhe API 73's supported-city list.
	City string `json:"city"`
}

// GetCurrentWeatherOutput contains only Juhe's current conditions; optional observations may be empty.
type GetCurrentWeatherOutput struct {
	// City is the provider's returned city name.
	City string `json:"city"`
	// WeatherText is the current condition description.
	WeatherText string `json:"weatherText"`
	// WeatherCode is the condition ID from Juhe's weather-type list.
	WeatherCode string `json:"weatherCode"`
	// TemperatureCelsius is the optional current temperature in Celsius.
	TemperatureCelsius string `json:"temperatureCelsius"`
	// HumidityPercent is the optional relative humidity percentage.
	HumidityPercent string `json:"humidityPercent"`
	// WindDirection is the optional wind direction text.
	WindDirection string `json:"windDirection"`
	// WindPower is the optional wind power text.
	WindPower string `json:"windPower"`
	// AirQualityIndex is the optional AQI text.
	AirQualityIndex string `json:"airQualityIndex"`
}

// GetCurrentWeatherOperation reads one current-weather observation.
type GetCurrentWeatherOperation struct{ client *Client }

// Definition returns the generated current-weather contract.
func (GetCurrentWeatherOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrentWeatherDefinition
}

// Invoke looks up the city and classifies provider and transport outcomes.
func (operation GetCurrentWeatherOperation) Invoke(call sdkgo.Call, input GetCurrentWeatherInput) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	if strings.TrimSpace(input.City) == "" || strings.TrimSpace(input.City) != input.City || len(input.City) > 128 {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "city is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	// Juhe documents GET and form POST; the form body keeps the key out of the URL.
	form := url.Values{"key": {credentials.APIKey.Reveal()}, "city": {input.City}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/simpleWeather/query", strings.NewReader(form.Encode()))
	if err != nil {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "weather request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Juhe request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return currentWeatherRetry(sdkgo.FailureRateLimit, "Juhe rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Juhe is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Juhe response exceeded the configured limit")
	}
	if err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Juhe response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureProviderRejection, "Juhe rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		ErrorCode *int `json:"error_code"`
		Result    *struct {
			City     string `json:"city"`
			Realtime struct {
				Info        string `json:"info"`
				Wid         string `json:"wid"`
				Temperature string `json:"temperature"`
				Humidity    string `json:"humidity"`
				Direct      string `json:"direct"`
				Power       string `json:"power"`
				AQI         string `json:"aqi"`
			} `json:"realtime"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.ErrorCode == nil {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "Juhe returned an invalid response")
	}
	if *wire.ErrorCode != 0 {
		return classifyJuheWeatherError(*wire.ErrorCode)
	}
	if wire.Result == nil || wire.Result.City == "" || wire.Result.Realtime.Info == "" || wire.Result.Realtime.Wid == "" {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "Juhe response lacks current weather fields")
	}
	output := GetCurrentWeatherOutput{City: wire.Result.City, WeatherText: wire.Result.Realtime.Info, WeatherCode: wire.Result.Realtime.Wid, TemperatureCelsius: wire.Result.Realtime.Temperature, HumidityPercent: wire.Result.Realtime.Humidity, WindDirection: wire.Result.Realtime.Direct, WindPower: wire.Result.Realtime.Power, AirQualityIndex: wire.Result.Realtime.AQI}
	return sdkgo.NewQueryBranch(GetCurrentWeatherBranchFetched, output, nil, sdkgo.Receipt{})
}

func classifyJuheWeatherError(code int) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	switch code {
	case 207303, 10014, 10020:
		return currentWeatherRetry(sdkgo.FailureAvailability, "Juhe weather service is temporarily unavailable", 0)
	case 10011:
		return currentWeatherRetry(sdkgo.FailureRateLimit, "Juhe limited the caller IP", 0)
	case 10001, 10003, 10009:
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureAuthentication, "Juhe rejected the API key")
	case 10002, 10005, 10007, 10008:
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureAuthorization, "Juhe denied API access")
	case 10012, 10013:
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureQuotaExhausted, "Juhe daily quota is exhausted")
	case 207301, 207302:
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureValidation, "Juhe rejected or did not find the city")
	default:
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureProviderRejection, "Juhe rejected the weather request")
	}
}
func isValidAPIKey(value string) bool {
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
func currentWeatherBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "juhe", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryBranch(branch, GetCurrentWeatherOutput{}, &failure, sdkgo.Receipt{})
}
func currentWeatherRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "juhe", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryRetry[GetCurrentWeatherOutput](failure, delay)
}
