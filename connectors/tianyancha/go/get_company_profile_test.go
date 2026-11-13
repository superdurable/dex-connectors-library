// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tianyancha

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "tianyancha", Name: "tianyancha"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tianyancha-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-company-profile" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(token any) any         { return context.Background().Value(token) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetCompanyProfileRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://open.api.tianyancha.com/services/v4/open/baseinfoV3", request.URL.Scheme+"://"+request.URL.Host+request.URL.Path)
		require.Equal(t, "平安银行股份有限公司", request.URL.Query().Get("keyword"))
		require.Len(t, request.URL.Query(), 1)
		require.Equal(t, "test-secret", request.Header.Get("Authorization"))
		return reply(200, `{"error_code":0,"reason":"ok","result":{"id":199557844,"name":"平安银行股份有限公司","regStatus":"存续","creditCode":"91440300192185379H","industry":"货币金融服务","regCapital":"1142489.4787万人民币","companyOrgType":"股份有限公司(上市)","staffList":{"result":[{"name":"private"}]},"email":"private@example.com"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetCompanyProfile(), testConnection, GetCompanyProfileInput{Keyword: "平安银行股份有限公司"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetCompanyProfileBranchFound, result.Branch)
	require.Equal(t, GetCompanyProfileOutput{CompanyID: 199557844, Name: "平安银行股份有限公司", RegistrationStatus: "存续", CreditCode: "91440300192185379H", Industry: "货币金融服务", RegisteredCapital: "1142489.4787万人民币", CompanyType: "股份有限公司(上市)"}, result.Value)
}
func TestGetCompanyProfileClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"no data", 200, `{"error_code":300000,"reason":"none"}`, GetCompanyProfileBranchNoData, false},
		{"invalid token", 200, `{"error_code":300002,"reason":"test-secret"}`, GetCompanyProfileBranchProviderRejected, false},
		{"permission", 200, `{"error_code":300005}`, GetCompanyProfileBranchProviderRejected, false},
		{"HTTP unauthorized", 401, `{}`, GetCompanyProfileBranchProviderRejected, false},
		{"HTTP forbidden", 403, `{}`, GetCompanyProfileBranchProviderRejected, false},
		{"quota", 200, `{"error_code":300006}`, GetCompanyProfileBranchProviderRejected, false},
		{"frequency", 200, `{"error_code":300004}`, "", true},
		{"system", 200, `{"error_code":300001}`, "", true},
		{"HTTP rate", 429, `{}`, "", true},
		{"HTTP unavailable", 503, `{}`, "", true},
		{"missing code", 200, `{}`, GetCompanyProfileBranchInvalidResponse, false},
		{"missing company", 200, `{"error_code":0}`, GetCompanyProfileBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetCompanyProfileBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetCompanyProfile(), testConnection, GetCompanyProfileInput{Keyword: "example"})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
			}
		})
	}
}
func TestGetCompanyProfileLocalValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	client := newTestClient(t, transport, 0)
	for _, keyword := range []string{"", " bad", "bad\nvalue"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetCompanyProfile(), testConnection, GetCompanyProfileInput{Keyword: keyword})
		require.NoError(t, err)
		require.Equal(t, GetCompanyProfileBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetCompanyProfile(), testConnection, GetCompanyProfileInput{Keyword: "example"})
	require.NoError(t, err)
	require.Equal(t, GetCompanyProfileBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetCompanyProfile(), testConnection, GetCompanyProfileInput{Keyword: "example"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
