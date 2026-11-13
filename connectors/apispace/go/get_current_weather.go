// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apispace

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

// GetCurrentWeatherInput selects one city by the provider's documented area code.
type GetCurrentWeatherInput struct {
	// AreaCode is a city ID from the APISpace weather product's city list.
	AreaCode string `json:"areaCode"`
}

// CurrentWeather contains selected public observations for one city.
type CurrentWeather struct {
	// City is the provider's city display name.
	City string `json:"city"`
	// Condition is the current weather description.
	Condition string `json:"condition"`
	// TemperatureCelsius is the current air temperature in degrees Celsius.
	TemperatureCelsius float64 `json:"temperatureCelsius"`
	// HumidityPercent is the relative humidity from 0 to 100 percent.
	HumidityPercent int `json:"humidityPercent"`
	// UpdatedAtBeijing is the provider update time in Beijing local time.
	UpdatedAtBeijing string `json:"updatedAtBeijing"`
}

// GetCurrentWeatherOperation reads current weather for one city.
type GetCurrentWeatherOperation struct{ client *Client }

// Definition returns the generated current-weather contract.
func (GetCurrentWeatherOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrentWeatherDefinition
}

// Invoke queries APISpace and classifies provider, protocol, and transport outcomes.
func (operation GetCurrentWeatherOperation) Invoke(call sdkgo.Call, input GetCurrentWeatherInput) sdkgo.QueryAttempt[CurrentWeather] {
	if !validAreaCode(input.AreaCode) {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "APISpace area code must be 1 to 32 ASCII letters or digits")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAPIToken(credentials.APIToken.Reveal()) {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureAuthentication, "APISpace API token is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/456456/weather/v001/now?areacode="+url.QueryEscape(input.AreaCode), nil)
	if err != nil {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "weather request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization-Type", "apikey")
	request.Header.Set("X-APISpace-Token", credentials.APIToken.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return weatherRetry(sdkgo.FailureAvailability, "APISpace weather request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestEntityTooLarge {
		return weatherRetry(sdkgo.FailureRateLimit, "APISpace rate limited weather requests", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return weatherRetry(sdkgo.FailureAvailability, "APISpace weather is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "APISpace response exceeded the configured limit")
	}
	if err != nil {
		return weatherRetry(sdkgo.FailureAvailability, "APISpace response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return weatherBranch(GetCurrentWeatherBranchProviderRejected, classifyWeatherHTTP(response.StatusCode), "APISpace rejected weather query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	weather, status, err := decodeCurrentWeather(body)
	if err != nil {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "APISpace returned an invalid weather response")
	}
	if status != 0 {
		return weatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureProviderRejection, "APISpace rejected weather query with status "+strconv.Itoa(status))
	}
	return sdkgo.NewQueryBranch(GetCurrentWeatherBranchFound, weather, nil, sdkgo.Receipt{})
}

func decodeCurrentWeather(body []byte) (CurrentWeather, int, error) {
	var wire struct {
		Status *int `json:"status"`
		Result *struct {
			Location *struct {
				Name string `json:"name"`
			} `json:"location"`
			Realtime *struct {
				Text string   `json:"text"`
				Temp *float64 `json:"temp"`
				RH   *int     `json:"rh"`
			} `json:"realtime"`
			LastUpdate string `json:"last_update"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Status == nil {
		return CurrentWeather{}, 0, errors.New("invalid APISpace JSON")
	}
	if *wire.Status != 0 {
		return CurrentWeather{}, *wire.Status, nil
	}
	if wire.Result == nil || wire.Result.Location == nil || wire.Result.Realtime == nil || wire.Result.Location.Name == "" || wire.Result.Realtime.Text == "" || wire.Result.Realtime.Temp == nil || wire.Result.Realtime.RH == nil || *wire.Result.Realtime.RH < 0 || *wire.Result.Realtime.RH > 100 || wire.Result.LastUpdate == "" {
		return CurrentWeather{}, 0, errors.New("invalid APISpace weather result")
	}
	return CurrentWeather{City: wire.Result.Location.Name, Condition: wire.Result.Realtime.Text, TemperatureCelsius: *wire.Result.Realtime.Temp, HumidityPercent: *wire.Result.Realtime.RH, UpdatedAtBeijing: wire.Result.LastUpdate}, 0, nil
}
func validAreaCode(code string) bool {
	if len(code) < 1 || len(code) > 32 {
		return false
	}
	for _, character := range code {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z') {
			return false
		}
	}
	return true
}
func validAPIToken(token string) bool {
	if token == "" || len(token) > 2048 || strings.TrimSpace(token) != token {
		return false
	}
	for _, character := range token {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyWeatherHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403, 416:
		return sdkgo.FailureAuthorization
	case 400, 404:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func weatherBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CurrentWeather] {
	failure := sdkgo.Failure{Kind: kind, Provider: "apispace", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryBranch(branch, CurrentWeather{}, &failure, sdkgo.Receipt{})
}
func weatherRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[CurrentWeather] {
	failure := sdkgo.Failure{Kind: kind, Provider: "apispace", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryRetry[CurrentWeather](failure, delay)
}
