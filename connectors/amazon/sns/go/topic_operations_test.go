// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const exampleTopicARN = "arn:aws:sns:eu-west-1:123456789012:alerts"

func TestCreateTopicSignsAndReturnsExistingOrNewTopic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "CreateTopic", request.URL.Query().Get("Action"))
		require.Equal(t, "alerts", request.URL.Query().Get("Name"))
		require.Contains(t, request.Header.Get("Authorization"), "/eu-west-1/sns/aws4_request")
		_, err := response.Write([]byte(`<CreateTopicResponse xmlns="https://sns.amazonaws.com/doc/2010-03-31/"><CreateTopicResult><TopicArn>` + exampleTopicARN + `</TopicArn></CreateTopicResult></CreateTopicResponse>`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).CreateTopic(), testConnection, CreateTopicInput{Name: "alerts"})
	require.NoError(t, err)
	require.Equal(t, CreateTopicBranchCreated, result.Branch)
	require.Equal(t, exampleTopicARN, result.Value.TopicARN)
}

func TestDeleteAndSetTopicAttribute(t *testing.T) {
	for _, operation := range []string{"DeleteTopic", "SetTopicAttributes"} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			require.Equal(t, operation, request.URL.Query().Get("Action"))
			require.Equal(t, exampleTopicARN, request.URL.Query().Get("TopicArn"))
			if operation == "SetTopicAttributes" {
				require.Equal(t, "DisplayName", request.URL.Query().Get("AttributeName"))
				require.Equal(t, "Operations", request.URL.Query().Get("AttributeValue"))
			}
			_, err := response.Write([]byte(`<` + operation + `Response xmlns="https://sns.amazonaws.com/doc/2010-03-31/"/>`))
			require.NoError(t, err)
		}))
		if operation == "DeleteTopic" {
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).DeleteTopic(), testConnection, TopicARNInput{TopicARN: exampleTopicARN})
			require.NoError(t, err)
			require.Equal(t, DeleteTopicBranchDeleted, result.Branch)
		} else {
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).SetTopicAttribute(), testConnection, SetTopicAttributeInput{TopicARN: exampleTopicARN, AttributeName: "DisplayName", AttributeValue: "Operations"})
			require.NoError(t, err)
			require.Equal(t, SetTopicAttributeBranchUpdated, result.Branch)
		}
		server.Close()
	}
}

func TestTopicWritesClassifyRejectionAndRetry(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(status) }))
		client := newTestClient(t, server.URL, 0)
		for _, invoke := range []func() (sdkgo.BranchID, error){
			func() (sdkgo.BranchID, error) {
				result, err := sdkgo.RunMutation(queryContext{}, client.CreateTopic(), testConnection, CreateTopicInput{Name: "alerts"})
				return result.Branch, err
			},
			func() (sdkgo.BranchID, error) {
				result, err := sdkgo.RunMutation(queryContext{}, client.DeleteTopic(), testConnection, TopicARNInput{TopicARN: exampleTopicARN})
				return result.Branch, err
			},
			func() (sdkgo.BranchID, error) {
				result, err := sdkgo.RunMutation(queryContext{}, client.SetTopicAttribute(), testConnection, SetTopicAttributeInput{TopicARN: exampleTopicARN, AttributeName: "DisplayName", AttributeValue: "Operations"})
				return result.Branch, err
			},
		} {
			branch, err := invoke()
			if status == http.StatusForbidden {
				require.NoError(t, err)
				require.Equal(t, sdkgo.BranchID("providerRejected"), branch)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			}
		}
		server.Close()
	}
}
