// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package variflight

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "variflight", Name: "variflight"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "variflight-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-flight-status" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{AppID: sdkgo.NewSecretString("test-app"), AppSecurity: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}

var flightInput = GetFlightStatusInput{FlightNumber: "GX8915", DepartureDate: "2026-11-13"}

const flightJSON = `[{"FlightNo":"GX8915","FlightState":"schedule","FlightStateNum":0,"FlightDeptimePlanDate":"2026-11-13 08:20:00","FlightDeptimeDate":"","AircraftNumber":"secret-aircraft"}]`

func TestFlightSignatureFixedVector(t *testing.T) {
	parameters := url.Values{"appid": {"test-app"}, "date": {"2026-11-13"}, "fnum": {"GX8915"}, "lang": {"EN"}}
	require.Equal(t, "64a2586610dc0f478324b575cb9820a5", flightSignature(parameters, "test-secret"))
}
func TestGetFlightStatusSignedRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, flightStatusEndpoint, request.URL.Scheme+"://"+request.URL.Host+request.URL.Path)
		require.Equal(t, "test-app", request.URL.Query().Get("appid"))
		require.Equal(t, "GX8915", request.URL.Query().Get("fnum"))
		require.Equal(t, "2026-11-13", request.URL.Query().Get("date"))
		require.Equal(t, "EN", request.URL.Query().Get("lang"))
		require.Equal(t, "64a2586610dc0f478324b575cb9820a5", request.URL.Query().Get("token"))
		require.Empty(t, request.Header.Get("Authorization"))
		return reply(200, flightJSON), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetFlightStatus(), testConnection, flightInput)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetFlightStatusBranchFound, result.Branch)
	require.Equal(t, GetFlightStatusOutput{Total: 1, Flights: []FlightStatus{{FlightNumber: "GX8915", State: "schedule", StateCode: 0, ScheduledDeparture: "2026-11-13 08:20:00"}}}, result.Value)
}
func TestGetFlightStatusOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"no data", 200, `{"error_code":10,"error":"test-secret"}`, GetFlightStatusBranchNoData, false},
		{"denied", 200, `{"error_code":2,"error":"test-secret"}`, GetFlightStatusBranchProviderRejected, false},
		{"bad token", 200, `{"error_code":3,"error":"test-secret"}`, GetFlightStatusBranchProviderRejected, false},
		{"provider error", 200, `{"error_code":500}`, "", true},
		{"throttled", 429, `{}`, "", true},
		{"server", 503, `{}`, "", true},
		{"malformed", 200, `{"unexpected":"test-secret"}`, GetFlightStatusBranchInvalidResponse, false},
		{"bad flight", 200, `[{"FlightNo":"GX8915"}]`, GetFlightStatusBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetFlightStatusBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetFlightStatus(), testConnection, flightInput)
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				require.NotContains(t, retry.Error(), "test-app")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
				require.NotContains(t, result.Failure.Message, "test-app")
			}
		})
	}
}
func TestGetFlightStatusValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("test-secret test-app") })
	client := newTestClient(t, transport, 0)
	for _, input := range []GetFlightStatusInput{{FlightNumber: "GX8915", DepartureDate: "2026-13-13"}, {FlightNumber: "GX8915\n", DepartureDate: "2026-11-13"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetFlightStatus(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetFlightStatusBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	_, err := sdkgo.RunQuery(queryContext{}, client.GetFlightStatus(), testConnection, flightInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
	require.NotContains(t, retry.Error(), "test-app")
}
func TestGetFlightStatusBoundsResponse(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetFlightStatus(), testConnection, flightInput)
	require.NoError(t, err)
	require.Equal(t, GetFlightStatusBranchInvalidResponse, result.Branch)
}
