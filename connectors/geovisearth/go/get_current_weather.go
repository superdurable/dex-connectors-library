// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package geovisearth

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const weatherPath = "/v2/cn/area/basic"

var areaCodePattern = regexp.MustCompile(`^WTX_CH[0-9]{9}$`)

// GetCurrentWeatherInput selects an official area code or longitude,latitude coordinate.
type GetCurrentWeatherInput struct {
	// Location is a GEOVIS Earth area code such as WTX_CH101300201, or longitude,latitude in decimal degrees.
	Location string `json:"location"`
}

// GetCurrentWeatherOutput contains the basic current-weather observation.
type GetCurrentWeatherOutput struct {
	// AreaCode is the area code, blank for coordinate lookups.
	AreaCode string `json:"areaCode"`
	// Weather is the Chinese weather condition description.
	Weather string `json:"weather"`
	// WeatherCode is the provider's weather phenomenon code.
	WeatherCode string `json:"weatherCode"`
	// TemperatureCelsius is air temperature in degrees Celsius.
	TemperatureCelsius float64 `json:"temperatureCelsius"`
	// RelativeHumidityPercent is relative humidity from 0 to 100 percent.
	RelativeHumidityPercent int `json:"relativeHumidityPercent"`
	// WindDirection is the human-readable wind direction.
	WindDirection string `json:"windDirection"`
	// WindForce is the human-readable wind-force level.
	WindForce string `json:"windForce"`
	// SurfacePressureHpa is surface pressure in hectopascals.
	SurfacePressureHpa float64 `json:"surfacePressureHpa"`
	// ObservedAt is the provider's local observation timestamp formatted yyyyMMddHHmmss.
	ObservedAt string `json:"observedAt"`
}

// GetCurrentWeatherOperation reads one GEOVIS Earth weather observation.
type GetCurrentWeatherOperation struct{ client *Client }

// Definition returns the generated weather Query contract.
func (GetCurrentWeatherOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrentWeatherDefinition
}

// Invoke reads current weather and classifies provider and transport outcomes.
func (operation GetCurrentWeatherOperation) Invoke(call sdkgo.Call, input GetCurrentWeatherInput) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	if !isValidLocation(input.Location) {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "location must be an area code or longitude,latitude coordinate")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidToken(credentials.Token.Reveal()) {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureAuthentication, "GEOVIS Earth token is unavailable or invalid")
	}
	values := url.Values{}
	values.Set("token", credentials.Token.Reveal())
	values.Set("location", input.Location)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+weatherPath+"?"+values.Encode(), nil)
	if err != nil {
		return weatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "weather request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return weatherRetry(sdkgo.FailureAvailability, "GEOVIS Earth request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return weatherRetry(sdkgo.FailureRateLimit, "GEOVIS Earth rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return weatherRetry(sdkgo.FailureAvailability, "GEOVIS Earth temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "weather response exceeded the configured limit")
	}
	if err != nil {
		return weatherRetry(sdkgo.FailureAvailability, "weather response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return weatherBranch(GetCurrentWeatherBranchProviderRejected, kind, "GEOVIS Earth rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Status *int `json:"status"`
		Date   struct {
			Time string `json:"time"`
		} `json:"date"`
		Result *struct {
			Weather                 string  `json:"wp"`
			WeatherCode             string  `json:"wp_code"`
			TemperatureCelsius      float64 `json:"tem"`
			RelativeHumidityPercent *int    `json:"rh"`
			WindDirection           string  `json:"wd_desc"`
			WindForce               string  `json:"ws_desc"`
			SurfacePressureHpa      float64 `json:"prs"`
		} `json:"result"`
		Location struct {
			AreaCode string `json:"areaCode"`
		} `json:"location"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(&wire) != nil || wire.Status == nil {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "GEOVIS Earth returned invalid weather data")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "GEOVIS Earth returned trailing weather data")
	}
	if *wire.Status != 0 {
		return weatherBranch(GetCurrentWeatherBranchProviderRejected, sdkgo.FailureProviderRejection, "GEOVIS Earth rejected the weather request, status "+strconv.Itoa(*wire.Status))
	}
	if wire.Result == nil || wire.Result.Weather == "" || wire.Result.WeatherCode == "" || wire.Result.RelativeHumidityPercent == nil || *wire.Result.RelativeHumidityPercent < 0 || *wire.Result.RelativeHumidityPercent > 100 || wire.Result.WindDirection == "" || wire.Result.WindForce == "" || wire.Result.SurfacePressureHpa <= 0 || !isFinite(wire.Result.SurfacePressureHpa) || !isFinite(wire.Result.TemperatureCelsius) || len(wire.Date.Time) != 14 {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "GEOVIS Earth returned incomplete weather observation")
	}
	if _, err := time.Parse("20060102150405", wire.Date.Time); err != nil {
		return weatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "GEOVIS Earth returned invalid observation time")
	}
	output := GetCurrentWeatherOutput{AreaCode: wire.Location.AreaCode, Weather: wire.Result.Weather, WeatherCode: wire.Result.WeatherCode, TemperatureCelsius: wire.Result.TemperatureCelsius, RelativeHumidityPercent: *wire.Result.RelativeHumidityPercent, WindDirection: wire.Result.WindDirection, WindForce: wire.Result.WindForce, SurfacePressureHpa: wire.Result.SurfacePressureHpa, ObservedAt: wire.Date.Time}
	return sdkgo.NewQueryBranch(GetCurrentWeatherBranchFetched, output, nil, sdkgo.Receipt{})
}
func isFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func isValidLocation(location string) bool {
	if areaCodePattern.MatchString(location) {
		return true
	}
	parts := strings.Split(location, ",")
	if len(parts) != 2 {
		return false
	}
	longitude, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || !isFinite(longitude) || longitude < -180 || longitude > 180 {
		return false
	}
	latitude, err := strconv.ParseFloat(parts[1], 64)
	return err == nil && isFinite(latitude) && latitude >= -90 && latitude <= 90 && strings.TrimSpace(location) == location
}
func isValidToken(token string) bool {
	if token == "" || len(token) > 512 {
		return false
	}
	for _, character := range token {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func weatherBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "geovisearth", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryBranch(branch, GetCurrentWeatherOutput{}, &failure, sdkgo.Receipt{})
}
func weatherRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "geovisearth", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryRetry[GetCurrentWeatherOutput](failure, delay)
}
