// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/internal/sqsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewDerivesTheRegionalEndpointAndRejectsUnsafeConfiguration(t *testing.T) {
	credentials := staticCredentials("")
	for _, region := range []string{"us-east-1", "eu-west-1", "us-gov-west-1", "cn-north-1", "eusc-de-east-1"} {
		_, err := sqs.New(sqs.Config{Region: region}, credentials)
		require.NoError(t, err, region)
	}
	for name, config := range map[string]sqs.Config{
		"a compatible Region without an endpoint": {Region: "elasticmq"},
		"a Region that is not a Region code":      {Region: "eu-west-1/sqs"},
		"a plain HTTP remote endpoint":            {Endpoint: "http://sqs.example.com", Region: "us-east-1"},
		"an endpoint with user information":       {Endpoint: "https://user:secret@sqs.example.com", Region: "us-east-1"},
		"an endpoint with a query":                {Endpoint: "https://sqs.example.com?x=1", Region: "us-east-1"},
		"an invalid default queue":                {DefaultQueueURL: "https://sqs.us-east-1.amazonaws.com/orders"},
		"more than ten messages per receive":      {MaxMessagesPerReceive: 11},
		"a wait above 20 seconds":                 {ReceiveWaitSeconds: 21},
	} {
		_, err := sqs.New(config, credentials)
		require.Error(t, err, name)
	}
	_, err := sqs.New(sqs.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
}

func TestDefaultsKeepTheManifestValues(t *testing.T) {
	require.Equal(t, sqs.Config{Region: "us-east-1", MaxMessagesPerReceive: 10, ReceiveWaitSeconds: 20, MaxResponseBytes: 4 << 20}, sqs.DefaultConfig())
}

func TestAnAmazonQueueURLInAnotherRegionIsADefectBeforeAnyRequest(t *testing.T) {
	client, err := sqs.New(sqs.Config{Region: "eu-west-1"}, staticCredentials(""))
	require.NoError(t, err)
	result, err := runMutation(t, newDexContext("wrong-region"), client.ReceiveMessages(), sqs.ReceiveMessagesInput{
		QueueURL: "https://sqs.us-east-2.amazonaws.com/123456789012/orders",
	})
	require.NoError(t, err)
	require.Equal(t, sqs.ReceiveMessagesBranchDefect, result.Branch)
	require.Equal(t, "queueUrl is in Region us-east-2 but the connection signs for eu-west-1; set region to us-east-2", result.Failure.Message)
}

func TestEveryRequestIsASignedJSONProtocolPostThatNeverCarriesTheSecret(t *testing.T) {
	queues := newFakeQueues(t, func(config *sqsfake.Config) { config.SessionToken = testSessionToken })
	client, err := sqs.New(sqs.Config{Endpoint: queues.URL, Region: testRegion, DefaultQueueURL: queues.QueueURL(ordersQueue)},
		staticCredentials(testSessionToken))
	require.NoError(t, err)
	queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	context := newDexContext("signed")
	received, err := runMutation(t, context, client.ReceiveMessages(), sqs.ReceiveMessagesInput{WaitTimeSeconds: seconds(0)})
	require.NoError(t, err)
	handle := received.Value.Messages[0].ReceiptHandle
	_, err = runMutation(t, newDexContext("signed-change"), client.ChangeMessageVisibility(), sqs.ChangeMessageVisibilityInput{ReceiptHandle: handle, VisibilityTimeoutSeconds: 60})
	require.NoError(t, err)
	_, err = runMutation(t, newDexContext("signed-delete"), client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: handle})
	require.NoError(t, err)

	require.Zero(t, queues.SignatureErrorCount())
	requests := queues.Requests()
	require.Equal(t, []string{"ReceiveMessage", "ChangeMessageVisibility", "DeleteMessage"},
		[]string{requests[0].Action, requests[1].Action, requests[2].Action})
	for _, request := range requests {
		require.Equal(t, "application/x-amz-json-1.0", request.Header.Get("Content-Type"))
		require.Equal(t, testSessionToken, request.Header.Get("X-Amz-Security-Token"))
		authorization := request.Header.Get("Authorization")
		require.Contains(t, authorization, "/"+testRegion+"/sqs/aws4_request")
		require.Contains(t, authorization, "content-type;host;x-amz-date;x-amz-security-token;x-amz-target")
		require.NotContains(t, authorization, testSecretAccessKey)
		require.NotContains(t, string(request.Body), testSecretAccessKey)
	}
}

func TestErrorResponsesClassifyByCodeWithoutProviderMessageText(t *testing.T) {
	for name, test := range map[string]struct {
		response  sqsfake.Response
		branch    sdkgo.BranchID
		kind      sdkgo.FailureKind
		retryKind sdkgo.FailureKind
		message   string
	}{
		"throttling":                {response: sqsfake.Response{StatusCode: http.StatusBadRequest, Code: "ThrottlingException"}, retryKind: sdkgo.FailureRateLimit},
		"a KMS throttle":            {response: sqsfake.Response{StatusCode: http.StatusBadRequest, Code: "KmsThrottled"}, retryKind: sdkgo.FailureRateLimit},
		"HTTP 429":                  {response: sqsfake.Response{StatusCode: http.StatusTooManyRequests}, retryKind: sdkgo.FailureRateLimit},
		"an internal error":         {response: sqsfake.Response{StatusCode: http.StatusInternalServerError, Code: "InternalError"}, retryKind: sdkgo.FailureAvailability},
		"an unavailable service":    {response: sqsfake.Response{StatusCode: http.StatusServiceUnavailable}, retryKind: sdkgo.FailureAvailability},
		"an unknown access key":     {response: sqsfake.Response{StatusCode: http.StatusForbidden, Code: "InvalidClientTokenId"}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "replace the access key"},
		"an expired session token":  {response: sqsfake.Response{StatusCode: http.StatusForbidden, Code: "ExpiredToken"}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureAuthentication},
		"a skewed clock":            {response: sqsfake.Response{StatusCode: http.StatusForbidden, Code: "RequestTimeTooSkewed"}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "Worker clock"},
		"a missing IAM permission":  {response: sqsfake.Response{StatusCode: http.StatusForbidden, Code: "AccessDenied"}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		"a KMS key the queue lacks": {response: sqsfake.Response{StatusCode: http.StatusBadRequest, Code: "KmsAccessDenied"}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		"too many in-flight":        {response: sqsfake.Response{StatusCode: http.StatusForbidden, Code: "OverLimit"}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureProviderRejection, message: "too many in-flight messages"},
		"an invalid parameter":      {response: sqsfake.Response{StatusCode: http.StatusBadRequest, Code: "InvalidParameterValue"}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		"an error without a body":   {response: sqsfake.Response{StatusCode: http.StatusBadRequest}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureProviderRejection, message: "HTTP 400"},
		"a legacy queue code only": {response: sqsfake.Response{StatusCode: http.StatusBadRequest, RawBody: []byte("not json"),
			Header: http.Header{"X-Amzn-Query-Error": {"AWS.SimpleQueueService.NonExistentQueue;Sender"}}}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureNotFound, message: "check queueUrl"},
		"a code that is not a token": {response: sqsfake.Response{StatusCode: http.StatusBadRequest,
			RawBody: []byte(`{"__type":"com.amazonaws.sqs#Not a code SENTINEL","message":"SENTINEL"}`)}, branch: sqs.ReceiveMessagesBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
	} {
		t.Run(name, func(t *testing.T) {
			queues := newFakeQueues(t)
			queues.Intercept(func(sqsfake.Request) *sqsfake.Response { return &test.response })
			result, err := runMutation(t, newDexContext("error"), newFakeClient(t, queues).ReceiveMessages(), sqs.ReceiveMessagesInput{WaitTimeSeconds: seconds(0)})
			if test.retryKind != "" {
				requireRetry(t, err, test.retryKind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.message)
			requireNoSecrets(t, result)
		})
	}
}

func TestRetryAfterIsHonoredForAThrottledRequest(t *testing.T) {
	queues := newFakeQueues(t)
	queues.Intercept(func(sqsfake.Request) *sqsfake.Response {
		return &sqsfake.Response{StatusCode: http.StatusServiceUnavailable, Code: "ServiceUnavailable", Header: http.Header{"Retry-After": {"7"}}}
	})
	_, err := runMutation(t, newDexContext("retry-after"), newFakeClient(t, queues).DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: "AQEB-handle"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
}

func TestUnavailableOrInvalidCredentialsSendNothing(t *testing.T) {
	queues := newFakeQueues(t)
	for name, credentials := range map[string]sqs.Credentials{
		"no credential":                  {},
		"an access key ID with a slash":  {AccessKeyID: sdkgo.NewSecretString("AKIA/X"), SecretAccessKey: sdkgo.NewSecretString("s")},
		"a secret with a control byte":   {AccessKeyID: sdkgo.NewSecretString("AKIAX"), SecretAccessKey: sdkgo.NewSecretString("s\x00")},
		"a session token with a newline": {AccessKeyID: sdkgo.NewSecretString("ASIAX"), SecretAccessKey: sdkgo.NewSecretString("s"), SessionToken: sdkgo.NewSecretString("t\n")},
	} {
		client, err := sqs.New(sqs.Config{Endpoint: queues.URL, Region: testRegion, DefaultQueueURL: queues.QueueURL(ordersQueue)},
			sdkgo.StaticCredentialProvider[sqs.Credentials]{testConnection: credentials})
		require.NoError(t, err)
		result, err := runMutation(t, newDexContext("credentials"), client.ReceiveMessages(), sqs.ReceiveMessagesInput{})
		require.NoError(t, err, name)
		require.Equal(t, sqs.ReceiveMessagesBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind, name)
		require.False(t, strings.Contains(result.Failure.Message, "\x00"), name)
	}
	require.Zero(t, queues.RequestCount(""))
}

func TestAWrongSigningRegionIsRejectedAsCredentials(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues, func(config *sqs.Config) { config.Region = "us-west-2" })
	result, err := runMutation(t, newDexContext("region"), client.ReceiveMessages(), sqs.ReceiveMessagesInput{WaitTimeSeconds: seconds(0)})
	require.NoError(t, err)
	require.Equal(t, sqs.ReceiveMessagesBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "SignatureDoesNotMatch")
}
