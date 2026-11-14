// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qingcloud

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "qingcloud", Name: "qingcloud"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "qingcloud-provider-test" }
func (queryContext) StepExecutionID() string     { return "describe-zones" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{AccessKeyID: "QYACCESSKEYIDEXAMPLE", SecretAccessKey: sdkgo.NewSecretString("SECRETACCESSKEY")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}

func TestHmacSHA256SignatureMatchesOfficialVector(t *testing.T) {
	canonical := "access_key_id=QYACCESSKEYIDEXAMPLE&action=RunInstances&count=1&image_id=centos64x86a&instance_name=demo&instance_type=small_b&login_mode=passwd&login_passwd=QingCloud20130712&signature_method=HmacSHA256&signature_version=1&time_stamp=2013-08-27T14%3A30%3A10Z&version=1&vxnets.1=vxnet-0&zone=pek3a"
	require.Equal(t, "byjccvWIvAftaq+oublemagH3bYAlDWxxLFAzAsyslw=", signCanonicalQuery("SECRETACCESSKEY", "GET", "/iaas/", canonical))
}
func TestDescribeZonesSignsCanonicalQueryAndLimitsOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/iaas/", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "SECRETACCESSKEY")
		params := request.URL.Query()
		require.Equal(t, "DescribeZones", params.Get("action"))
		require.Equal(t, "100", params.Get("limit"))
		require.Equal(t, "HmacSHA256", params.Get("signature_method"))
		require.Equal(t, "1", params.Get("signature_version"))
		require.Equal(t, "1", params.Get("version"))
		require.Equal(t, "QYACCESSKEYIDEXAMPLE", params.Get("access_key_id"))
		_, err := time.Parse("2006-01-02T15:04:05Z", params.Get("time_stamp"))
		require.NoError(t, err)
		signature := params.Get("signature")
		params.Del("signature")
		require.Equal(t, signCanonicalQuery("SECRETACCESSKEY", "GET", "/iaas/", params.Encode()), signature)
		require.Contains(t, request.URL.RawQuery, "time_stamp=")
		require.Contains(t, request.URL.RawQuery, "%3A")
		_, err = io.WriteString(writer, `{"action":"DescribeZonesResponse","ret_code":0,"total_count":2,"zone_set":[{"zone_id":"pek3a","status":"active"},{"zone_id":"gd2","status":"faulty"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).DescribeZones(), testConnection, DescribeZonesInput{})
	require.NoError(t, err)
	require.Equal(t, DescribeZonesBranchListed, result.Branch)
	require.Equal(t, ZonesPage{Zones: []Zone{{ZoneID: "pek3a", Status: "active"}, {ZoneID: "gd2", Status: "faulty"}}, TotalCount: 2}, result.Value)
}
func TestDescribeZonesClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty list", 200, `{"action":"DescribeZonesResponse","ret_code":0,"total_count":0,"zone_set":[]}`, DescribeZonesBranchListed, false},
		{"authentication", 200, `{"ret_code":1200}`, DescribeZonesBranchProviderRejected, false},
		{"expired clock", 200, `{"ret_code":1300}`, DescribeZonesBranchProviderRejected, false},
		{"daily quota", 200, `{"ret_code":2500}`, DescribeZonesBranchProviderRejected, false},
		{"server busy", 200, `{"ret_code":5100}`, "", true},
		{"server internal", 200, `{"ret_code":5000}`, "", true},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"forbidden", 403, `{}`, DescribeZonesBranchProviderRejected, false},
		{"missing return code", 200, `{"action":"DescribeZonesResponse","total_count":0,"zone_set":[]}`, DescribeZonesBranchInvalidResponse, false},
		{"wrong action", 200, `{"action":"OtherResponse","ret_code":0,"total_count":0,"zone_set":[]}`, DescribeZonesBranchInvalidResponse, false},
		{"invalid zone", 200, `{"action":"DescribeZonesResponse","ret_code":0,"total_count":1,"zone_set":[{"zone_id":"pek3a","status":"unknown"}]}`, DescribeZonesBranchInvalidResponse, false},
		{"inconsistent count", 200, `{"action":"DescribeZonesResponse","ret_code":0,"total_count":0,"zone_set":[{"zone_id":"pek3a","status":"active"}]}`, DescribeZonesBranchInvalidResponse, false},
		{"trailing JSON", 200, `{"action":"DescribeZonesResponse","ret_code":0,"total_count":0} {}`, DescribeZonesBranchInvalidResponse, false},
		{"malformed", 200, `{"action":`, DescribeZonesBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).DescribeZones(), testConnection, DescribeZonesInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestDescribeZonesBoundsResponseAndRetriesTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"action":"DescribeZonesResponse","ret_code":0,"total_count":0}`)
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).DescribeZones(), testConnection, DescribeZonesInput{})
	require.NoError(t, err)
	require.Equal(t, DescribeZonesBranchInvalidResponse, result.Branch)
	endpoint := server.URL
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).DescribeZones(), testConnection, DescribeZonesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestDescribeZonesRejectsMissingCredentials(t *testing.T) {
	_, endpointError := New(Config{BaseURL: "https://api.qingcloud.com/iaas/"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, endpointError)
	for _, value := range []string{"", " bad", "bad\nvalue"} {
		require.False(t, validAccessKeyID(value))
	}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.DescribeZones(), testConnection, DescribeZonesInput{})
	require.NoError(t, err)
	require.Equal(t, DescribeZonesBranchDefect, result.Branch)
	require.False(t, strings.Contains(result.Failure.Message, "SECRETACCESSKEY"))
}
func TestCanonicalQueryEncoding(t *testing.T) {
	params := url.Values{"time_stamp": {"2013-08-27T14:30:10Z"}, "action": {"DescribeZones"}}
	require.Equal(t, "action=DescribeZones&time_stamp=2013-08-27T14%3A30%3A10Z", params.Encode())
}
