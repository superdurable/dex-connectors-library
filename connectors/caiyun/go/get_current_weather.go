// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package caiyun

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GetCurrentWeatherInput locates one observation; mainland China uses GCJ-02 coordinates, elsewhere WGS84.
type GetCurrentWeatherInput struct {
	// Longitude is degrees east from -180 through 180.
	Longitude float64 `json:"longitude"`
	// Latitude is degrees north from -90 through 90.
	Latitude float64 `json:"latitude"`
}

// GetCurrentWeatherOutput contains metric weather values without credentials or raw provider data.
type GetCurrentWeatherOutput struct {
	// TemperatureCelsius is air temperature at two meters.
	TemperatureCelsius float64 `json:"temperatureCelsius"`
	// RelativeHumidity is a fraction from zero to one.
	RelativeHumidity float64 `json:"relativeHumidity"`
	// SkyCondition is Caiyun's weather condition code.
	SkyCondition string `json:"skyCondition"`
	// WindSpeedMetersPerSecond is wind speed at ten meters.
	WindSpeedMetersPerSecond float64 `json:"windSpeedMetersPerSecond"`
}

// GetCurrentWeatherOperation reads one Caiyun realtime observation.
type GetCurrentWeatherOperation struct{ client *Client }

// Definition returns the generated Query contract.
func (GetCurrentWeatherOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrentWeatherDefinition
}

// Invoke signs and sends one metered weather request and classifies its outcome.
func (operation GetCurrentWeatherOperation) Invoke(call sdkgo.Call, input GetCurrentWeatherInput) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	if !isFiniteInRange(input.Longitude, -180, 180) || !isFiniteInRange(input.Latitude, -90, 90) {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "longitude or latitude is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAppKey(credentials.AppKey.Reveal()) || !isValidAppSecret(credentials.AppSecret.Reveal()) {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Caiyun request nonce could not be generated", 0)
	}
	nonce := hex.EncodeToString(nonceBytes)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	appKey := credentials.AppKey.Reveal()
	// Caiyun v2.6 requires the App Key in the path: https://docs.caiyunapp.com/weather-api/v2/v2.6/auth.html
	path := "/v2.6/" + appKey + "/" + strconv.FormatFloat(input.Longitude, 'f', -1, 64) + "," + strconv.FormatFloat(input.Latitude, 'f', -1, 64) + "/realtime"
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+path, nil)
	if err != nil {
		return currentWeatherBranch(GetCurrentWeatherBranchDefect, sdkgo.FailureValidation, "weather request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-cy-nonce", nonce)
	request.Header.Set("x-cy-timestamp", timestamp)
	request.Header.Set("x-cy-signature", signWeatherRequest(appKey, credentials.AppSecret.Reveal(), path, "", nonce, timestamp))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Caiyun request failed; a prior attempt may consume another plan call", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Caiyun is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return currentWeatherRetry(sdkgo.FailureRateLimit, "Caiyun rate or quota limit reached", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return currentWeatherBranch(GetCurrentWeatherBranchProviderRejected, classifyWeatherHTTPStatus(response.StatusCode), "Caiyun rejected the weather request")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Caiyun response exceeded the configured limit")
	}
	if err != nil {
		return currentWeatherRetry(sdkgo.FailureAvailability, "Caiyun response was interrupted", 0)
	}
	var wire struct {
		Status string `json:"status"`
		Result struct {
			Realtime struct {
				Status      string   `json:"status"`
				Temperature *float64 `json:"temperature"`
				Humidity    *float64 `json:"humidity"`
				Skycon      string   `json:"skycon"`
				Wind        struct {
					Speed *float64 `json:"speed"`
				} `json:"wind"`
			} `json:"realtime"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Status != "ok" || wire.Result.Realtime.Status != "ok" || wire.Result.Realtime.Temperature == nil || wire.Result.Realtime.Humidity == nil || wire.Result.Realtime.Wind.Speed == nil || wire.Result.Realtime.Skycon == "" || !isFiniteInRange(*wire.Result.Realtime.Humidity, 0, 1) || !isFiniteInRange(*wire.Result.Realtime.Temperature, -150, 150) || !isFiniteInRange(*wire.Result.Realtime.Wind.Speed, 0, 200) {
		return currentWeatherBranch(GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, "Caiyun returned invalid realtime weather data")
	}
	output := GetCurrentWeatherOutput{TemperatureCelsius: *wire.Result.Realtime.Temperature, RelativeHumidity: *wire.Result.Realtime.Humidity, SkyCondition: wire.Result.Realtime.Skycon, WindSpeedMetersPerSecond: *wire.Result.Realtime.Wind.Speed}
	return sdkgo.NewQueryBranch(GetCurrentWeatherBranchFetched, output, nil, sdkgo.Receipt{})
}
func signWeatherRequest(appKey, appSecret, path, canonicalQuery, nonce, timestamp string) string {
	signed := strings.Join([]string{"GET", path, canonicalQuery, appKey, nonce, timestamp}, ":")
	digest := hmac.New(sha256.New, []byte(appSecret))
	_, _ = digest.Write([]byte(signed))
	return base64.URLEncoding.EncodeToString(digest.Sum(nil))
}
func isFiniteInRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}
func isValidAppKey(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character != '-' && character != '_' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}
func isValidAppSecret(value string) bool {
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
func classifyWeatherHTTPStatus(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case 400, 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 422:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func currentWeatherBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "caiyun", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryBranch(branch, GetCurrentWeatherOutput{}, &failure, sdkgo.Receipt{})
}
func currentWeatherRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetCurrentWeatherOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "caiyun", Operation: "getCurrentWeather", Message: message}
	return sdkgo.NewQueryRetry[GetCurrentWeatherOutput](failure, delay)
}
