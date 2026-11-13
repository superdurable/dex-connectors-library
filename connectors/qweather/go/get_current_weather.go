// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qweather

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

// GetCurrentWeatherInput locates the weather sample in decimal degrees.
type GetCurrentWeatherInput struct {
	// Latitude is geographic latitude in decimal degrees, from -90 to 90.
	Latitude float64 `json:"latitude"`
	// Longitude is geographic longitude in decimal degrees, from -180 to 180.
	Longitude float64 `json:"longitude"`
}

// GetCurrentWeatherOutput contains current condition, temperature, time, and provider attribution.
type GetCurrentWeatherOutput struct {
	// ConditionText is the localized human-readable weather condition.
	ConditionText string `json:"conditionText"`
	// ConditionCode is the QWeather condition code.
	ConditionCode string `json:"conditionCode"`
	// TemperatureValue is the measured temperature in TemperatureUnit.
	TemperatureValue float64 `json:"temperatureValue"`
	// TemperatureUnit is the provider's temperature unit, usually °C or °F.
	TemperatureUnit string `json:"temperatureUnit"`
	// ValidTime is the ISO 8601 time represented by this observation.
	ValidTime string `json:"validTime"`
	// Attributions must accompany displayed weather data under provider guidance.
	Attributions []string `json:"attributions"`
}

// GetCurrentWeatherOperation reads weather without modifying provider state.
type GetCurrentWeatherOperation struct{ client *Client }

// Definition returns the generated current-weather contract.
func (GetCurrentWeatherOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrentWeatherDefinition
}

// Invoke queries the account-specific host and returns bounded weather fields.
func (operation GetCurrentWeatherOperation) Invoke(call sdkgo.Call, input GetCurrentWeatherInput) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	if !isValidCoordinates(input) {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "latitude or longitude is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIHost(credentials.APIHost.Reveal()) || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureAuthentication, "API Host or key is unavailable or invalid")
	}
	path := "/weather/v1/current/" + strconv.FormatFloat(input.Latitude, 'f', 6, 64) + "/" + strconv.FormatFloat(input.Longitude, 'f', 6, 64)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, "https://"+credentials.APIHost.Reveal()+path, nil)
	if err != nil {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "weather request is invalid")
	}
	request.Header.Set("X-QW-Api-Key", credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return weatherRetry(sdkgo.FailureAvailability, "QWeather request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return weatherRetry(sdkgo.FailureAvailability, "QWeather is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "QWeather response exceeded the configured limit")
	}
	if err != nil {
		return weatherRetry(sdkgo.FailureAvailability, "QWeather response was interrupted", 0)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		if isMonthlyLimit(body) {
			return weatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureRateLimit, "QWeather monthly request limit was reached")
		}
		return weatherRetry(sdkgo.FailureRateLimit, "QWeather rate limited the request", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyWeatherStatus(response.StatusCode)
	}
	var wire struct {
		Metadata struct {
			Attributions []string `json:"attributions"`
		} `json:"metadata"`
		ValidTime string `json:"validTime"`
		Condition struct {
			Text string `json:"text"`
			Code string `json:"code"`
		} `json:"condition"`
		Temperature struct {
			Value *float64 `json:"value"`
			Unit  string   `json:"unit"`
		} `json:"temperature"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Condition.Text == "" || wire.Condition.Code == "" || wire.Temperature.Value == nil || math.IsNaN(*wire.Temperature.Value) || math.IsInf(*wire.Temperature.Value, 0) || wire.Temperature.Unit == "" {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "QWeather returned invalid weather data")
	}
	if _, err := time.Parse(time.RFC3339, wire.ValidTime); err != nil {
		if _, minuteErr := time.Parse("2006-01-02T15:04Z07:00", wire.ValidTime); minuteErr != nil {
			return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "QWeather returned an invalid validTime")
		}
	}

	if len(wire.Metadata.Attributions) == 0 || len(wire.Metadata.Attributions) > 16 {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "QWeather returned invalid attributions")
	}
	for _, attribution := range wire.Metadata.Attributions {
		if attribution == "" || len(attribution) > 500 || strings.Contains(attribution, credentials.APIKey.Reveal()) || strings.Contains(attribution, credentials.APIHost.Reveal()) {
			return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "QWeather returned invalid attributions")
		}
	}
	if strings.Contains(wire.Condition.Text, credentials.APIKey.Reveal()) || strings.Contains(wire.Condition.Text, credentials.APIHost.Reveal()) {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "QWeather returned invalid weather data")
	}
	output := GetCurrentWeatherOutput{ConditionText: wire.Condition.Text, ConditionCode: wire.Condition.Code, TemperatureValue: *wire.Temperature.Value, TemperatureUnit: wire.Temperature.Unit, ValidTime: wire.ValidTime, Attributions: wire.Metadata.Attributions}
	return sdkgo.NewQueryBranch(GetCurrentWeatherBranchFound, output, nil, sdkgo.Receipt{})
}
func isValidCoordinates(input GetCurrentWeatherInput) bool {
	return !math.IsNaN(input.Latitude) && !math.IsInf(input.Latitude, 0) && input.Latitude >= -90 && input.Latitude <= 90 && !math.IsNaN(input.Longitude) && !math.IsInf(input.Longitude, 0) && input.Longitude >= -180 && input.Longitude <= 180
}
func isMonthlyLimit(body []byte) bool {
	var problem struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &problem) != nil {
		return false
	}
	return strings.Contains(strings.ToLower(problem.Error.Type), "over-monthly-limit")
}
func classifyWeatherStatus(status int) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return weatherBranch(GetCurrentWeatherBranchProviderRejected, kind, "QWeather rejected the request with HTTP "+strconv.Itoa(status))
}
func weatherBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qweather", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryBranch(branch, GetCurrentWeatherOutput{}, &failure, sdkgo.Receipt{})
}
func weatherRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qweather", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryRetry[GetCurrentWeatherOutput](failure, delay)
}
