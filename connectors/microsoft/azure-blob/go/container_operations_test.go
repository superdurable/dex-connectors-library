// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azureblob

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListContainersSignsAccountRequestAndParsesPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/", request.URL.Path)
		require.Equal(t, "list", request.URL.Query().Get("comp"))
		require.Equal(t, "doc", request.URL.Query().Get("prefix"))
		require.NotEmpty(t, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte("<EnumerationResults><Containers><Container><Name>docs</Name></Container></Containers><NextMarker>next</NextMarker></EnumerationResults>"))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListContainers(), testConnection, ListContainersInput{Prefix: "doc"})
	require.NoError(t, err)
	require.Equal(t, ListContainersBranchListed, result.Branch)
	require.Equal(t, ContainersPage{Names: []string{"docs"}, NextMarker: "next"}, result.Value)
}

func TestGetContainerReadsProperties(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodHead, request.Method)
		require.Equal(t, "container", request.URL.Query().Get("restype"))
		writer.Header().Set("ETag", "\""+"0x1"+"\"")
		writer.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetContainer(), testConnection, GetContainerInput{Container: "docs"})
	require.NoError(t, err)
	require.Equal(t, GetContainerBranchFound, result.Branch)
	require.Equal(t, "Mon, 02 Jan 2006 15:04:05 GMT", result.Value.LastModified)
}

func TestCreateContainerReportsExistingName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "container", request.URL.Query().Get("restype"))
		if request.Method == http.MethodPut {
			require.NotEmpty(t, request.Header.Get("x-ms-meta-dex_create_call_id"))
			writer.Header().Set("x-ms-error-code", "ContainerAlreadyExists")
			writer.WriteHeader(http.StatusConflict)
			return
		}
		require.Equal(t, http.MethodHead, request.Method)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).CreateContainer(), testConnection, CreateContainerInput{Container: "docs"})
	require.NoError(t, err)
	require.Equal(t, CreateContainerBranchAlreadyExists, result.Branch)
}

func TestCreateContainerRecoversLostCreatedResponse(t *testing.T) {
	var putCount atomic.Int32
	var createCallID string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "container", request.URL.Query().Get("restype"))
		if request.Method == http.MethodPut {
			callID := request.Header.Get("x-ms-meta-dex_create_call_id")
			require.NotEmpty(t, callID)
			if putCount.Add(1) == 1 {
				createCallID = callID
				writer.WriteHeader(http.StatusCreated)
				connection, _, err := writer.(http.Hijacker).Hijack()
				require.NoError(t, err)
				require.NoError(t, connection.Close())
				return
			}
			require.Equal(t, createCallID, callID)
			writer.Header().Set("x-ms-error-code", "ContainerAlreadyExists")
			writer.WriteHeader(http.StatusConflict)
			return
		}
		require.Equal(t, http.MethodHead, request.Method)
		writer.Header().Set("x-ms-meta-dex_create_call_id", createCallID)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	operation := newTestClient(t, server.URL, 0).CreateContainer()
	input := CreateContainerInput{Container: "docs"}
	_, err := sdkgo.RunMutation(queryContext{}, operation, testConnection, input)
	require.Error(t, err)
	result, err := sdkgo.RunMutation(queryContext{}, operation, testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContainerBranchCreated, result.Branch)
	require.Equal(t, int32(2), putCount.Load())
}

func TestDeleteContainerAcceptsMissingName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodDelete, request.Method)
		require.Equal(t, "container", request.URL.Query().Get("restype"))
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).DeleteContainer(), testConnection, DeleteContainerInput{Container: "docs"})
	require.NoError(t, err)
	require.Equal(t, DeleteContainerBranchDeleted, result.Branch)
}
