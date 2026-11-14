// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yuandian

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

var testConnection = sdkgo.ConnectionRef{Provider: "yuandian", Name: "yuandian"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "yuandian-provider-test" }
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
		require.Equal(t, "https://open.chineselaw.com/open/rh_company_detail", request.URL.Scheme+"://"+request.URL.Host+request.URL.Path)
		require.Equal(t, "91110108MA0074PN30", request.URL.Query().Get("tyshxydm"))
		require.Len(t, request.URL.Query(), 1)
		require.Equal(t, "test-secret", request.Header.Get("X-API-Key"))
		return reply(200, `{"code":200,"data":{"企业ID":"sample-id","企业名称":"示例企业有限公司","统一社会信用代码":"91110108MA0074PN30","经营状态":"存续","行业":"商务服务业","企业类型":"有限责任公司","法定代表人":"私人姓名","联系电话":"123456"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetCompanyProfile(), testConnection, GetCompanyProfileInput{CreditCode: "91110108MA0074PN30"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetCompanyProfileBranchFound, result.Branch)
	require.Equal(t, GetCompanyProfileOutput{CompanyID: "sample-id", Name: "示例企业有限公司", CreditCode: "91110108MA0074PN30", RegistrationStatus: "存续", Industry: "商务服务业", CompanyType: "有限责任公司"}, result.Value)
}
func TestGetCompanyProfileClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"no data", 200, `{"code":200,"message":"未查询到相关数据"}`, GetCompanyProfileBranchNoData, false},
		{"invalid key", 200, `{"code":401}`, GetCompanyProfileBranchProviderRejected, false},
		{"permission", 200, `{"code":403}`, GetCompanyProfileBranchProviderRejected, false},
		{"HTTP unauthorized", 401, `{}`, GetCompanyProfileBranchProviderRejected, false},
		{"HTTP forbidden", 403, `{}`, GetCompanyProfileBranchProviderRejected, false},
		{"parameter", 200, `{"code":422}`, GetCompanyProfileBranchProviderRejected, false},
		{"rate", 200, `{"code":429}`, "", true},
		{"system", 200, `{"code":500}`, "", true},
		{"HTTP rate", 429, `{}`, "", true},
		{"HTTP unavailable", 503, `{}`, "", true},
		{"missing code", 200, `{}`, GetCompanyProfileBranchInvalidResponse, false},
		{"missing company", 200, `{"code":200}`, GetCompanyProfileBranchInvalidResponse, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetCompanyProfile(), testConnection, GetCompanyProfileInput{CreditCode: "91110108MA0074PN30"})
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
	for _, keyword := range []string{"", "bad", "bad\nvalue"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetCompanyProfile(), testConnection, GetCompanyProfileInput{CreditCode: keyword})
		require.NoError(t, err)
		require.Equal(t, GetCompanyProfileBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetCompanyProfile(), testConnection, GetCompanyProfileInput{CreditCode: "91110108MA0074PN30"})
	require.NoError(t, err)
	require.Equal(t, GetCompanyProfileBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetCompanyProfile(), testConnection, GetCompanyProfileInput{CreditCode: "91110108MA0074PN30"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
