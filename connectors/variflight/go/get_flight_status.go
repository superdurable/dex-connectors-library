// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package variflight

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var flightNumberPattern = regexp.MustCompile(`^[A-Z0-9]{3,8}$`)

// GetFlightStatusInput selects a flight number and local departure date.
type GetFlightStatusInput struct {
	// FlightNumber is the airline code and number, such as GX8915.
	FlightNumber string `json:"flightNumber"`
	// DepartureDate is the local flight departure date in YYYY-MM-DD format.
	DepartureDate string `json:"departureDate"`
}

// FlightStatus contains a small subset of the provider's flight data.
type FlightStatus struct {
	// FlightNumber is the provider's normalized flight number.
	FlightNumber string `json:"flightNumber"`
	// State is the provider's English flight status.
	State string `json:"state"`
	// StateCode is VariFlight's numeric flight status code.
	StateCode int `json:"stateCode"`
	// ScheduledDeparture is the planned local departure timestamp.
	ScheduledDeparture string `json:"scheduledDeparture"`
	// ActualDeparture is the actual local departure timestamp when available.
	ActualDeparture string `json:"actualDeparture"`
}

// GetFlightStatusOutput contains the provider record count and at most twenty flight statuses.
type GetFlightStatusOutput struct {
	// Total is the number of records returned by the provider.
	Total int `json:"total"`
	// Flights contains at most the first twenty records.
	Flights []FlightStatus `json:"flights"`
}

// GetFlightStatusOperation queries flight data using a partner signature.
type GetFlightStatusOperation struct{ client *Client }

// Definition returns the generated flight-status Query contract.
func (GetFlightStatusOperation) Definition() sdkgo.QueryDefinition { return GetFlightStatusDefinition }

// Invoke queries flight status and returns only bounded status fields.
func (operation GetFlightStatusOperation) Invoke(call sdkgo.Call, input GetFlightStatusInput) sdkgo.QueryAttempt[GetFlightStatusOutput] {
	if !flightNumberPattern.MatchString(input.FlightNumber) {
		return flightBranch(GetFlightStatusBranchDefect, sdkgo.FailureValidation, "VariFlight flight number is invalid")
	}
	date, err := time.Parse("2006-01-02", input.DepartureDate)
	if err != nil || date.Format("2006-01-02") != input.DepartureDate {
		return flightBranch(GetFlightStatusBranchDefect, sdkgo.FailureValidation, "VariFlight departure date must be YYYY-MM-DD")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppID.Reveal()) || !isValidCredential(credentials.AppSecurity.Reveal()) {
		return flightBranch(GetFlightStatusBranchDefect, sdkgo.FailureAuthentication, "VariFlight partner credentials are unavailable or invalid")
	}
	parameters := url.Values{"appid": {credentials.AppID.Reveal()}, "date": {input.DepartureDate}, "fnum": {input.FlightNumber}, "lang": {"EN"}}
	parameters.Set("token", flightSignature(parameters, credentials.AppSecurity.Reveal()))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, flightStatusEndpoint+"?"+parameters.Encode(), nil)
	if err != nil {
		return flightBranch(GetFlightStatusBranchDefect, sdkgo.FailureValidation, "VariFlight flight request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return flightRetry(sdkgo.FailureAvailability, "VariFlight flight request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return flightRetry(sdkgo.FailureRateLimit, "VariFlight rate limited flight request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return flightRetry(sdkgo.FailureAvailability, "VariFlight API is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return flightBranch(GetFlightStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "VariFlight rejected flight request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return flightBranch(GetFlightStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "VariFlight flight response exceeded the configured limit")
	}
	if err != nil {
		return flightRetry(sdkgo.FailureAvailability, "VariFlight flight response was interrupted", 0)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return flightBranch(GetFlightStatusBranchInvalidResponse, sdkgo.FailureProtocol, "VariFlight returned empty flight data")
	}
	if body[0] == '{' {
		var failure struct {
			ErrorCode *int `json:"error_code"`
		}
		if json.Unmarshal(body, &failure) != nil || failure.ErrorCode == nil {
			return flightBranch(GetFlightStatusBranchInvalidResponse, sdkgo.FailureProtocol, "VariFlight returned invalid error data")
		}
		if *failure.ErrorCode == 10 {
			return sdkgo.NewQueryBranch(GetFlightStatusBranchNoData, GetFlightStatusOutput{Flights: []FlightStatus{}}, nil, sdkgo.Receipt{})
		}
		if *failure.ErrorCode == 500 {
			return flightRetry(sdkgo.FailureAvailability, "VariFlight reported temporary error", 0)
		}
		return flightBranch(GetFlightStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "VariFlight rejected flight request with code "+strconv.Itoa(*failure.ErrorCode))
	}
	var records []struct {
		FlightNumber       string `json:"FlightNo"`
		State              string `json:"FlightState"`
		StateCode          *int   `json:"FlightStateNum"`
		ScheduledDeparture string `json:"FlightDeptimePlanDate"`
		ActualDeparture    string `json:"FlightDeptimeDate"`
	}
	if json.Unmarshal(body, &records) != nil || records == nil {
		return flightBranch(GetFlightStatusBranchInvalidResponse, sdkgo.FailureProtocol, "VariFlight returned invalid flight data")
	}
	flights := make([]FlightStatus, 0, min(len(records), 20))
	for _, record := range records {
		if record.FlightNumber == "" || record.State == "" || record.StateCode == nil || len(record.ScheduledDeparture) > 64 || len(record.ActualDeparture) > 64 || strings.ContainsAny(record.State, "\r\n\x00") {
			return flightBranch(GetFlightStatusBranchInvalidResponse, sdkgo.FailureProtocol, "VariFlight returned malformed flight status")
		}
		if len(flights) < 20 {
			flights = append(flights, FlightStatus{FlightNumber: record.FlightNumber, State: record.State, StateCode: *record.StateCode, ScheduledDeparture: record.ScheduledDeparture, ActualDeparture: record.ActualDeparture})
		}
	}
	return sdkgo.NewQueryBranch(GetFlightStatusBranchFound, GetFlightStatusOutput{Total: len(records), Flights: flights}, nil, sdkgo.Receipt{})
}

func flightSignature(parameters url.Values, appSecurity string) string {
	pairs := make([]string, 0, len(parameters))
	for _, key := range []string{"appid", "date", "fnum", "lang"} {
		pairs = append(pairs, key+"="+parameters.Get(key))
	}
	first := md5.Sum([]byte(strings.Join(pairs, "&") + appSecurity))
	firstHex := hex.EncodeToString(first[:])
	second := md5.Sum([]byte(firstHex))
	return hex.EncodeToString(second[:])
}
func flightBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetFlightStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "variflight", Operation: "getFlightStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetFlightStatusOutput{}, &failure, sdkgo.Receipt{})
}
func flightRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetFlightStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "variflight", Operation: "getFlightStatus", Message: message}
	return sdkgo.NewQueryRetry[GetFlightStatusOutput](failure, delay)
}
