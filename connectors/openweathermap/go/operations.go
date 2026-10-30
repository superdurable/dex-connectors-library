// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openweathermap

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// WeatherInput selects coordinates and temperature units.
type WeatherInput struct {
	// Latitude is degrees north, from -90 through 90.
	Latitude float64 `json:"latitude"`
	// Longitude is degrees east, from -180 through 180.
	Longitude float64 `json:"longitude"`
	// Units is standard, metric, or imperial; blank uses standard.
	Units string `json:"units,omitempty"`
}

// CurrentWeatherOutput is the current temperature and condition at a location.
type CurrentWeatherOutput struct {
	// City is the provider's location name.
	City string `json:"city"`
	// Temperature uses the requested units.
	Temperature float64 `json:"temperature"`
	// Description is the provider's weather condition text.
	Description string `json:"description"`
}

// ForecastEntry is one three-hour forecast point.
type ForecastEntry struct {
	// Timestamp is the forecast's UTC Unix time in seconds.
	Timestamp int64 `json:"timestamp"`
	// Temperature uses the requested units.
	Temperature float64 `json:"temperature"`
}

// ForecastOutput contains one five-day forecast page.
type ForecastOutput struct {
	// City is the provider's location name.
	City string `json:"city"`
	// Entries are the forecast's three-hour points.
	Entries []ForecastEntry `json:"entries"`
}

// GetCurrentWeatherOperation reads one current weather report.
type GetCurrentWeatherOperation struct{ client *Client }

// GetForecastOperation reads one five-day forecast.
type GetForecastOperation struct{ client *Client }

// GetCurrentWeather binds this operation to the client.
func (client *Client) GetCurrentWeather() GetCurrentWeatherOperation {
	return GetCurrentWeatherOperation{client}
}

// GetForecast binds this operation to the client.
func (client *Client) GetForecast() GetForecastOperation { return GetForecastOperation{client} }

// Definition returns the generated current-weather contract.
func (GetCurrentWeatherOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrentWeatherDefinition
}

// Definition returns the generated forecast contract.
func (GetForecastOperation) Definition() sdkgo.QueryDefinition { return GetForecastDefinition }

// Invoke requests current weather by coordinates.
func (operation GetCurrentWeatherOperation) Invoke(call sdkgo.Call, input WeatherInput) sdkgo.QueryAttempt[CurrentWeatherOutput] {
	branches := queryBranches{found: GetCurrentWeatherBranchFound, notFound: GetCurrentWeatherBranchNotFound, rejected: GetCurrentWeatherBranchProviderRejected, invalid: GetCurrentWeatherBranchInvalidResponse, defect: GetCurrentWeatherBranchDefect}
	query, err := weatherQuery(input)
	if err != nil {
		return queryBranch("getCurrentWeather", branches.defect, CurrentWeatherOutput{}, sdkgo.FailureValidation, err.Error())
	}
	return invokeJSONQuery(call, operation.client, "getCurrentWeather", branches, http.MethodGet, "/data/2.5/weather", query, nil, decodeCurrentWeather)
}

// Invoke requests a five-day forecast by coordinates.
func (operation GetForecastOperation) Invoke(call sdkgo.Call, input WeatherInput) sdkgo.QueryAttempt[ForecastOutput] {
	branches := queryBranches{found: GetForecastBranchFound, notFound: GetForecastBranchNotFound, rejected: GetForecastBranchProviderRejected, invalid: GetForecastBranchInvalidResponse, defect: GetForecastBranchDefect}
	query, err := weatherQuery(input)
	if err != nil {
		return queryBranch("getForecast", branches.defect, ForecastOutput{}, sdkgo.FailureValidation, err.Error())
	}
	return invokeJSONQuery(call, operation.client, "getForecast", branches, http.MethodGet, "/data/2.5/forecast", query, nil, decodeForecast)
}

func weatherQuery(input WeatherInput) (url.Values, error) {
	if math.IsNaN(input.Latitude) || math.IsInf(input.Latitude, 0) || input.Latitude < -90 || input.Latitude > 90 || math.IsNaN(input.Longitude) || math.IsInf(input.Longitude, 0) || input.Longitude < -180 || input.Longitude > 180 {
		return nil, fmt.Errorf("latitude or longitude is outside its degree range")
	}
	if input.Units == "" {
		input.Units = "standard"
	}
	if input.Units != "standard" && input.Units != "metric" && input.Units != "imperial" {
		return nil, fmt.Errorf("units must be standard, metric, or imperial")
	}
	return url.Values{"lat": {strconv.FormatFloat(input.Latitude, 'f', -1, 64)}, "lon": {strconv.FormatFloat(input.Longitude, 'f', -1, 64)}, "units": {input.Units}}, nil
}

func decodeCurrentWeather(body []byte) (CurrentWeatherOutput, error) {
	var wire struct {
		Name string `json:"name"`
		Main struct {
			Temp *float64 `json:"temp"`
		} `json:"main"`
		Weather []struct {
			Description string `json:"description"`
		} `json:"weather"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CurrentWeatherOutput{}, err
	}
	if wire.Main.Temp == nil || len(wire.Weather) == 0 {
		return CurrentWeatherOutput{}, fmt.Errorf("weather fields missing")
	}
	return CurrentWeatherOutput{City: wire.Name, Temperature: *wire.Main.Temp, Description: wire.Weather[0].Description}, nil
}

func decodeForecast(body []byte) (ForecastOutput, error) {
	var wire struct {
		City struct {
			Name string `json:"name"`
		} `json:"city"`
		List []struct {
			DT   int64 `json:"dt"`
			Main struct {
				Temp *float64 `json:"temp"`
			} `json:"main"`
		} `json:"list"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ForecastOutput{}, err
	}
	if wire.List == nil {
		return ForecastOutput{}, fmt.Errorf("forecast list missing")
	}
	output := ForecastOutput{City: wire.City.Name, Entries: make([]ForecastEntry, 0, len(wire.List))}
	for _, item := range wire.List {
		if item.Main.Temp == nil || item.DT <= 0 {
			return ForecastOutput{}, fmt.Errorf("forecast point incomplete")
		}
		output.Entries = append(output.Entries, ForecastEntry{Timestamp: item.DT, Temperature: *item.Main.Temp})
	}
	return output, nil
}
