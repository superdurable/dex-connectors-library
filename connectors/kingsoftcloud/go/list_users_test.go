// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kingsoftcloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessKeyID = "test-access-key-id"
const testSecretAccessKey = "test-secret-access-key"

var testConnection = sdkgo.ConnectionRef{Provider: "kingsoftcloud", Name: "kingsoftcloud"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "kingsoftcloud-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-users" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {
		AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), SecretAccessKey: sdkgo.NewSecretString(testSecretAccessKey),
	}}, clockOption{now: func() time.Time { return time.Date(2021, 8, 12, 2, 47, 36, 0, time.UTC) }})
	require.NoError(t, err)
	return client
}

func TestPublishedSimplifiedSignatureVector(t *testing.T) {
	// https://docs.ksyun.com/documents/40299 publishes this canonical string and HMAC.
	query := url.Values{"Accesskey": {"AKLTXQVF0pOmS6aahIrD5r0B3Q"}, "Service": {"iam"}, "Action": {"CreateUser"}, "Version": {"2015-11-01"}, "Timestamp": {"2021-08-12T02:47:36Z"}, "SignatureVersion": {"1.0"}, "SignatureMethod": {"HMAC-SHA256"}, "UserName": {"Ttest"}, "RealName": {"周四测试"}, "Email": {"zsce@kkingsoft.com"}, "Remark": {"~ce shi*%#|+"}}
	require.Equal(t, "Accesskey=AKLTXQVF0pOmS6aahIrD5r0B3Q&Action=CreateUser&Email=zsce%40kkingsoft.com&RealName=%E5%91%A8%E5%9B%9B%E6%B5%8B%E8%AF%95&Remark=~ce%20shi%2A%25%23%7C%2B&Service=iam&SignatureMethod=HMAC-SHA256&SignatureVersion=1.0&Timestamp=2021-08-12T02%3A47%3A36Z&UserName=Ttest&Version=2015-11-01", canonicalIAMQuery(query))
	require.Equal(t, "fc9088ab845949dac4040be9b7ce7859068b5c21d4c400fec8ee0cefb777f659", signIAMQuery(query, "OMovU5PTLh6y9E9Ioe3K411jt99VqyQSBXgAcDYlo49R3lvUIzb6e/efZCFDmtFlzw=="))
}

func TestListUsersRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/", request.URL.Path)
		query := request.URL.Query()
		for key, want := range map[string]string{"Accesskey": testAccessKeyID, "Action": "ListUsers", "Service": "iam", "Region": "cn-beijing-6", "Version": "2015-11-01", "SignatureVersion": "1.0", "SignatureMethod": "HMAC-SHA256", "Timestamp": "2021-08-12T02:47:36Z", "MaxItems": "25", "Marker": "next-page="} {
			require.Equal(t, want, query.Get(key), key)
		}
		signature := query.Get("Signature")
		query.Del("Signature")
		require.Equal(t, signIAMQuery(query, testSecretAccessKey), signature)
		require.NotContains(t, request.URL.String(), testSecretAccessKey)
		_, err := writer.Write([]byte(`{"ListUserResult":{"Users":{"member":[{"UserId":"id-1","UserName":"operator","Email":"private@example.com","Phone":"private-phone"}]},"IsTruncated":true,"Marker":"another-page="},"RequestId":"request-1"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListUsers(), testConnection, ListUsersInput{MaxItems: 25, Marker: "next-page="})
	require.NoError(t, err)
	require.Equal(t, ListUsersBranchListed, result.Branch)
	require.Equal(t, []IAMUser{{UserID: "id-1", UserName: "operator"}}, result.Value.Users)
	require.True(t, result.Value.HasMore)
	require.Equal(t, "another-page=", result.Value.NextMarker)
	require.EqualValues(t, 1, calls.Load())
}

func TestListUsersOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty page", 200, `{"ListUserResult":{"Users":{"member":[]},"IsTruncated":false}}`, ListUsersBranchListed, false},
		{"terminal marker", 200, `{"ListUserResult":{"Users":{"member":[]},"IsTruncated":false,"Marker":"unused-token"}}`, ListUsersBranchListed, false},
		{"rejected", 403, `{"Error":{"Code":"AccessDenied","Message":"private provider data"}}`, ListUsersBranchProviderRejected, false},
		{"rate limited", 429, `private provider data`, "", true},
		{"unavailable", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, ListUsersBranchInvalidResponse, false},
		{"missing users", 200, `{"ListUserResult":{}}`, ListUsersBranchInvalidResponse, false},
		{"missing marker", 200, `{"ListUserResult":{"Users":{"member":[]},"IsTruncated":true}}`, ListUsersBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListUsers(), testConnection, ListUsersInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAccessKeyID)
				require.NotContains(t, err.Error(), testSecretAccessKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}

func TestInvalidInputSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, input := range []ListUsersInput{{MaxItems: -1}, {MaxItems: 101}, {Marker: strings.Repeat("x", 321)}, {Marker: " leading"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListUsers(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListUsersBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
