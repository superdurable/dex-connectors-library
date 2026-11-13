// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fxiaoke

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "fxiaoke", Name: "fxiaoke"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "fxiaoke-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-object-metadata" }
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
	credentials := Credentials{AppID: "FSAID_123", AppSecret: sdkgo.NewSecretString("app-secret"), PermanentCode: sdkgo.NewSecretString("permanent-code")}
	client, err := New(Config{EmployeeID: "employee-123", MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func tokenResponse() *http.Response {
	return reply(200, `{"errorCode":0,"accessToken":"access-secret","expiresIn":7084,"ea":"enterprise-123"}`)
}
func TestListObjectMetadataRequestAndTokenCache(t *testing.T) {
	tokenCalls, listCalls := 0, 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "open.fxiaoke.com", request.URL.Host)
		require.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, request.URL.Query().Get("thirdTraceId"))
		if request.URL.Path == "/oauth2.0/token" {
			tokenCalls++
			require.Equal(t, http.MethodPost, request.Method)
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"appId":"FSAID_123","appSecret":"app-secret","permanentCode":"permanent-code","grantType":"app_secret"}`, string(body))
			require.Empty(t, request.Header.Get("Authorization"))
			return tokenResponse(), nil
		}
		listCalls++
		require.Equal(t, "/cgi/crm/v2/object/list", request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "Bearer access-secret", request.Header.Get("Authorization"))
		require.Equal(t, "enterprise-123", request.Header.Get("x-fs-ea"))
		require.Equal(t, "employee-123", request.Header.Get("x-fs-userid"))
		return reply(200, `{"errorDescription":"success","data":{"objects":[{"describeApiName":"AccountObj","describeDisplayName":"客户","isActive":true,"privateCustomer":"secret-customer"}]}}`), nil
	})
	client := newTestClient(t, transport, 0)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListObjectMetadata(), testConnection, ListObjectMetadataInput{})
		require.NoError(t, err)
		require.Equal(t, ListObjectMetadataBranchListed, result.Branch)
		require.Equal(t, []ObjectMetadata{{APIName: "AccountObj", DisplayName: "客户", Active: true}}, result.Value.Objects)
	}
	require.Equal(t, 1, tokenCalls)
	require.Equal(t, 2, listCalls)
}
func TestListObjectMetadataProviderOutcomes(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty list", 200, `{"data":{"objects":[]}}`, ListObjectMetadataBranchListed, false},
		{"permission", 200, `{"errorCode":15003,"errorDescription":"app-secret"}`, ListObjectMetadataBranchProviderRejected, false},
		{"quota", 200, `{"errorCode":30003}`, ListObjectMetadataBranchProviderRejected, false},
		{"rate limit code", 200, `{"errorCode":30004}`, "", true},
		{"invalid shape", 200, `{"data":{}}`, ListObjectMetadataBranchInvalidResponse, false},
		{"http unauthorized", 401, `{"message":"app-secret"}`, ListObjectMetadataBranchProviderRejected, false},
		{"http unavailable", 503, `{}`, "", true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/oauth2.0/token" {
					return tokenResponse(), nil
				}
				return reply(scenario.status, scenario.body), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListObjectMetadata(), testConnection, ListObjectMetadataInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "app-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "app-secret")
			}
		})
	}
}
func TestListObjectMetadataTokenAndResponseFailures(t *testing.T) {
	scenarios := []struct {
		name        string
		tokenStatus int
		tokenBody   string
		branch      sdkgo.BranchID
		retry       bool
	}{
		{"bad credentials", 200, `{"errorCode":20006,"errorMessage":"app-secret"}`, ListObjectMetadataBranchProviderRejected, false},
		{"transient token", 200, `{"errorCode":-1}`, "", true},
		{"invalid token shape", 200, `{"errorCode":0,"accessToken":"access-secret","expiresIn":7084}`, ListObjectMetadataBranchInvalidResponse, false},
		{"token server unavailable", 503, `{}`, "", true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return reply(scenario.tokenStatus, scenario.tokenBody), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListObjectMetadata(), testConnection, ListObjectMetadataInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
		})
	}
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return reply(200, strings.Repeat("x", 100)), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListObjectMetadata(), testConnection, ListObjectMetadataInput{})
	require.NoError(t, err)
	require.Equal(t, ListObjectMetadataBranchInvalidResponse, result.Branch)
	require.Equal(t, 1, calls)
}
func TestListObjectMetadataRejectsUnsafeHostAndRedirect(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: "FSAID_123", AppSecret: sdkgo.NewSecretString("app-secret"), PermanentCode: sdkgo.NewSecretString("permanent-code")}}
	for _, host := range []string{"http://open.fxiaoke.com", "evil.example", "open-evil.fxiaoke.com.evil.example", "open-..fxiaoke.com"} {
		_, err := New(Config{CloudHost: host, EmployeeID: "employee-123"}, credentials)
		require.Error(t, err)
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListObjectMetadata(), testConnection, ListObjectMetadataInput{})
	require.NoError(t, err)
	require.Equal(t, ListObjectMetadataBranchProviderRejected, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("app-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListObjectMetadata(), testConnection, ListObjectMetadataInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "app-secret")
}

func TestListObjectMetadataSerializesTokenExchange(t *testing.T) {
	var tokenCalls atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/oauth2.0/token" {
			tokenCalls.Add(1)
			return tokenResponse(), nil
		}
		return reply(200, `{"data":{"objects":[]}}`), nil
	})
	client := newTestClient(t, transport, 0)
	var workers sync.WaitGroup
	errorsFound := make(chan error, 12)
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := sdkgo.RunQuery(queryContext{}, client.ListObjectMetadata(), testConnection, ListObjectMetadataInput{})
			errorsFound <- err
		}()
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), tokenCalls.Load())
}
