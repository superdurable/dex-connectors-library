// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestBucketReadsAndDeletion(t *testing.T) {
	var deleted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/storage/v1/b":
			require.Equal(t, "123456789", r.URL.Query().Get("project"))
			require.Equal(t, "5", r.URL.Query().Get("maxResults"))
			require.Equal(t, "report", r.URL.Query().Get("prefix"))
			require.Equal(t, "cursor", r.URL.Query().Get("pageToken"))
			_, err := w.Write([]byte(`{"items":[{"name":"report-bucket","location":"US"}],"nextPageToken":"next"}`))
			require.NoError(t, err)
		case r.Method == http.MethodGet && r.URL.Path == "/storage/v1/b/report-bucket":
			_, err := w.Write([]byte(`{"name":"report-bucket","location":"US"}`))
			require.NoError(t, err)
		case r.Method == http.MethodDelete && r.URL.Path == "/storage/v1/b/report-bucket":
			if deleted.Swap(true) {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListBuckets(), testConnection, ListBucketsInput{Project: "123456789", Prefix: "report", PageSize: 5, PageToken: "cursor"})
	require.NoError(t, err)
	require.Equal(t, ListBucketsBranchListed, page.Branch)
	require.Equal(t, BucketsPage{Buckets: []Bucket{{Name: "report-bucket", Location: "US"}}, NextPageToken: "next"}, page.Value)
	bucket, err := sdkgo.RunQuery(queryContext{}, client.GetBucket(), testConnection, GetBucketInput{Name: "report-bucket"})
	require.NoError(t, err)
	require.Equal(t, GetBucketBranchFound, bucket.Branch)
	for range 2 {
		result, err := sdkgo.RunMutation(singledispatchtest.NewContext("bucket-delete-flow", "delete-bucket-step"), client.DeleteBucket(), testConnection, DeleteBucketInput{Name: "report-bucket"})
		require.NoError(t, err)
		require.Equal(t, DeleteBucketBranchDeleted, result.Branch)
	}
}

func TestBucketCreationAndLabelUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			require.Equal(t, "/storage/v1/b", r.URL.Path)
			require.Equal(t, "123456789", r.URL.Query().Get("project"))
			_, err := w.Write([]byte(`{"name":"report-bucket","location":"US","projectNumber":"123456789"}`))
			require.NoError(t, err)
		case http.MethodPatch:
			require.Equal(t, "/storage/v1/b/report-bucket", r.URL.Path)
			_, err := w.Write([]byte(`{"name":"report-bucket","location":"US"}`))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	created, err := sdkgo.RunMutation(singledispatchtest.NewContext("bucket-create-flow", "create-step"), client.CreateBucket(), testConnection, CreateBucketInput{ProjectNumber: "123456789", Name: "report-bucket", Location: "US"})
	require.NoError(t, err)
	require.Equal(t, CreateBucketBranchCreated, created.Branch)
	require.Equal(t, "123456789", created.Value.ProjectNumber)
	updated, err := sdkgo.RunMutation(singledispatchtest.NewContext("bucket-label-flow", "update-step"), client.UpdateBucketLabels(), testConnection, UpdateBucketLabelsInput{Name: "report-bucket", Labels: map[string]string{"team": "reports"}})
	require.NoError(t, err)
	require.Equal(t, UpdateBucketLabelsBranchUpdated, updated.Branch)
}

func TestBucketCreationLimitsUnknownSends(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		sends.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	context := singledispatchtest.NewContext("bucket-unknown-flow", "create-step")
	input := CreateBucketInput{ProjectNumber: "123456789", Name: "report-bucket", Location: "US"}
	for range 2 {
		_, err := sdkgo.RunMutation(context, client.CreateBucket(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		context.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(context, client.CreateBucket(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateBucketBranchUncertain, result.Branch)
	require.EqualValues(t, 3, sends.Load())
}

func TestBucketBranchesAndRetries(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListBuckets(), testConnection, ListBucketsInput{Project: "123456789"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		_, err = sdkgo.RunMutation(singledispatchtest.NewContext("bucket-retry-flow", "delete-step"), client.DeleteBucket(), testConnection, DeleteBucketInput{Name: "report-bucket"})
		require.ErrorAs(t, err, &retry)
	}
	status.Store(http.StatusForbidden)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListBuckets(), testConnection, ListBucketsInput{Project: "123456789"})
	require.NoError(t, err)
	require.Equal(t, ListBucketsBranchProviderRejected, page.Branch)
	deleted, err := sdkgo.RunMutation(singledispatchtest.NewContext("bucket-reject-flow", "delete-step"), client.DeleteBucket(), testConnection, DeleteBucketInput{Name: "report-bucket"})
	require.NoError(t, err)
	require.Equal(t, DeleteBucketBranchProviderRejected, deleted.Branch)
	invalid, err := sdkgo.RunQuery(queryContext{}, client.GetBucket(), testConnection, GetBucketInput{Name: "bad/name"})
	require.NoError(t, err)
	require.Equal(t, GetBucketBranchDefect, invalid.Branch)
}

func TestBucketTransportFailureDoesNotExposeCredentials(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Bearer " + testAccessToken)
	})}
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(httpClient))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListBuckets(), testConnection, ListBucketsInput{Project: "123456789"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), testAccessToken)
}
