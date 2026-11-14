// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newayz

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

const weatherEndpoint = "https://api.newayz.com/location/scenario/v1/weather"

// GetCurrentWeatherInput selects one six-digit Chinese city or district code.
type GetCurrentWeatherInput struct {
	// CityCode is a six-digit administrative code such as 420100 for Wuhan.
	CityCode string `json:"cityCode"`
}

// GetCurrentWeatherOutput contains one WAYZ live weather observation.
type GetCurrentWeatherOutput struct {
	// Province is the province name.
	Province string `json:"province"`
	// City is the city name.
	City string `json:"city"`
	// CityCode is the administrative code returned by WAYZ.
	CityCode string `json:"cityCode"`
	// Weather is the current weather description.
	Weather string `json:"weather"`
	// TemperatureCelsius is the observed Celsius temperature as a decimal string.
	TemperatureCelsius string `json:"temperatureCelsius"`
	// WindDirection is the current wind direction.
	WindDirection string `json:"windDirection"`
	// WindPower is the provider wind scale string.
	WindPower string `json:"windPower"`
	// HumidityPercent is the relative humidity percent as a decimal string.
	HumidityPercent string `json:"humidityPercent"`
	// ReportTime is the provider observation timestamp string.
	ReportTime string `json:"reportTime"`
}

// GetCurrentWeatherOperation reads one current-weather observation.
type GetCurrentWeatherOperation struct{ client *Client }

// Definition returns the generated current-weather contract.
func (GetCurrentWeatherOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrentWeatherDefinition
}

// Invoke queries the WAYZ HTTPS weather API and excludes credentials and raw provider errors from results.
func (operation GetCurrentWeatherOperation) Invoke(call sdkgo.Call, input GetCurrentWeatherInput) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	if !isCityCode(input.CityCode) {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "WAYZ requires a six-digit city code")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccessKey(credentials.AccessKey.Reveal()) {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureAuthentication, "WAYZ credentials are unavailable or invalid")
	}
	query := url.Values{"access_key": {credentials.AccessKey.Reveal()}, "city": {input.CityCode}, "extensions": {"base"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, weatherEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "WAYZ weather request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "WAYZ weather request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return currentWeatherRetry(sdkgo.FailureRateLimit, "WAYZ rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return currentWeatherRetry(sdkgo.FailureAvailability, "WAYZ weather service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyWeatherHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "WAYZ response exceeded the configured limit")
	}
	if err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "WAYZ response was interrupted", 0)
	}
	var wire struct {
		Status   string `json:"status"`
		InfoCode string `json:"infocode"`
		Lives    []struct {
			Province      string `json:"province"`
			City          string `json:"city"`
			CityCode      string `json:"adcode"`
			Weather       string `json:"weather"`
			Temperature   string `json:"temperature"`
			WindDirection string `json:"winddirection"`
			WindPower     string `json:"windpower"`
			Humidity      string `json:"humidity"`
			ReportTime    string `json:"reporttime"`
		} `json:"lives"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Status == "" {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "WAYZ returned invalid weather data")
	}
	if wire.Status != "1" || wire.InfoCode != "10000" {
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureProviderRejection, "WAYZ rejected weather query with code "+safeProviderCode(wire.InfoCode))
	}
	if len(wire.Lives) != 1 || wire.Lives[0].CityCode != input.CityCode || wire.Lives[0].City == "" || wire.Lives[0].Weather == "" || wire.Lives[0].Temperature == "" {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "WAYZ returned incomplete weather data")
	}
	live := wire.Lives[0]
	output := GetCurrentWeatherOutput{Province: live.Province, City: live.City, CityCode: live.CityCode, Weather: live.Weather, TemperatureCelsius: live.Temperature, WindDirection: live.WindDirection, WindPower: live.WindPower, HumidityPercent: live.Humidity, ReportTime: live.ReportTime}
	return sdkgo.NewQueryBranch(GetCurrentWeatherBranchFetched, output, nil, sdkgo.Receipt{})
}

func isCityCode(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func isValidAccessKey(value string) bool {
	if value == "" || len(value) > 1024 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}

func safeProviderCode(value string) string {
	if len(value) > 16 || value == "" {
		return "unknown"
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return "unknown"
		}
	}
	return value
}

func classifyWeatherHTTPStatus(status int) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, kind, "WAYZ rejected weather query with HTTP "+strconv.Itoa(status))
}

func currentWeatherBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "newayz", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryBranch(branch, GetCurrentWeatherOutput{}, &failure, sdkgo.Receipt{})
}

func currentWeatherRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "newayz", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryRetry[GetCurrentWeatherOutput](failure, delay)
}
