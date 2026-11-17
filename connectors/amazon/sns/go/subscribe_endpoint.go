// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

var subscriptionEmailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
var subscriptionTargetARNPattern = regexp.MustCompile(`^arn:aws(-us-gov|-cn)?:(sqs|lambda):[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_./:-]+$`)

// SubscribeEndpointInput names a topic and a delivery destination.
type SubscribeEndpointInput struct {
	// TopicARN is the complete ARN from SNS Console > Topics.
	TopicARN string `json:"topicARN"`
	// Protocol is https, email, sqs, or lambda.
	Protocol string `json:"protocol"`
	// Endpoint is a public HTTPS URL, email address, SQS ARN, or Lambda ARN matching Protocol.
	Endpoint string `json:"endpoint"`
}

// SubscribeEndpointOperation subscribes one endpoint with bounded possible calls.
type SubscribeEndpointOperation struct{ client *Client }

// SubscribeEndpoint binds the Mutation to this client.
func (client *Client) SubscribeEndpoint() SubscribeEndpointOperation {
	return SubscribeEndpointOperation{client}
}

// Definition returns the generated Mutation contract.
func (SubscribeEndpointOperation) Definition() sdkgo.MutationDefinition {
	return SubscribeEndpointDefinition
}

// IdempotencyKey identifies attempts of one Step; SNS does not accept a deduplication key.
func (SubscribeEndpointOperation) IdempotencyKey(callID sdkgo.CallID, _ SubscribeEndpointInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke subscribes and reads back uncertain outcomes before a bounded resend.
func (operation SubscribeEndpointOperation) Invoke(call sdkgo.Call, input SubscribeEndpointInput) sdkgo.MutationAttempt[SubscriptionIdentity] {
	const operationID = "subscribeEndpoint"
	if !validSubscriptionEndpoint(input) {
		return subscriptionMutationBranch(operationID, SubscribeEndpointBranchDefect, sdkgo.FailureValidation, "SNS topic, protocol, or endpoint is invalid")
	}
	fence, err := singledispatch.NewFence(call, SubscribeEndpointDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return subscriptionMutationBranch(operationID, SubscribeEndpointBranchDefect, sdkgo.FailureLocalDefect, "SNS subscription fence could not be initialized")
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(SubscriptionIdentity{}, topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription checkpoint cannot be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		if value, found := operation.readBack(call, input); found {
			return sdkgo.NewMutationBranch(SubscribeEndpointBranchSubscribed, value, nil, topicReceipt(call, value.SubscriptionARN))
		}
		if fence.Decide().Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(SubscriptionIdentity{}, topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription could not be confirmed after the send cap"), sdkgo.Receipt{})
		}
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(SubscriptionIdentity{}, topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return subscriptionMutationBranch(operationID, SubscribeEndpointBranchDefect, sdkgo.FailureAuthentication, "SNS credentials are unavailable")
	}
	query := url.Values{"Action": {"Subscribe"}, "Version": {"2010-03-31"}, "TopicArn": {input.TopicARN}, "Protocol": {input.Protocol}, "Endpoint": {input.Endpoint}, "ReturnSubscriptionArn": {"true"}}
	send := &snsSubscribeSend{client: operation.client, call: call, query: query}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationUncertain(SubscriptionIdentity{}, topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(SubscriptionIdentity{}, topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS resend checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, kind, "SNS asked the connector to retry subscription"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return sdkgo.NewMutationUncertain(SubscriptionIdentity{}, topicOperationFailure(operationID, sdkgo.FailureProtocol, "SNS subscription response repeated a credential"), sdkgo.Receipt{})
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return subscriptionMutationBranch(operationID, SubscribeEndpointBranchProviderRejected, sdkgo.FailureProviderRejection, "SNS rejected the subscription")
	}
	var wire struct {
		XMLName xml.Name `xml:"SubscribeResponse"`
		Result  struct {
			ARN string `xml:"SubscriptionArn"`
		} `xml:"SubscribeResult"`
	}
	if xml.Unmarshal(response.body, &wire) != nil || wire.XMLName.Local != "SubscribeResponse" || !isSNSNamespace(wire.XMLName.Space) || !validateSubscriptionARN(wire.Result.ARN) {
		return sdkgo.NewMutationUncertain(SubscriptionIdentity{}, topicOperationFailure(operationID, sdkgo.FailureProtocol, "SNS accepted subscription without a usable ARN"), sdkgo.Receipt{})
	}
	value := SubscriptionIdentity{SubscriptionARN: wire.Result.ARN, TopicARN: input.TopicARN, Protocol: input.Protocol, Endpoint: input.Endpoint}
	return sdkgo.NewMutationBranch(SubscribeEndpointBranchSubscribed, value, nil, topicReceipt(call, wire.Result.ARN))
}

func (operation SubscribeEndpointOperation) readBack(call sdkgo.Call, input SubscribeEndpointInput) (SubscriptionIdentity, bool) {
	nextToken := ""
	for pageIndex := 0; pageIndex < 10; pageIndex++ {
		query := url.Values{"Action": {"ListSubscriptionsByTopic"}, "Version": {"2010-03-31"}, "TopicArn": {input.TopicARN}}
		if nextToken != "" {
			query.Set("NextToken", nextToken)
		}
		response := operation.client.request(call, query)
		if response.status != http.StatusOK || response.err != nil {
			return SubscriptionIdentity{}, false
		}
		page, err := decodeSubscriptionPage(response.body, "ListSubscriptionsByTopicResponse", "ListSubscriptionsByTopicResult")
		if err != nil {
			return SubscriptionIdentity{}, false
		}
		for _, item := range page.Subscriptions {
			if item.TopicARN == input.TopicARN && item.Protocol == input.Protocol && item.Endpoint == input.Endpoint && validateSubscriptionARN(item.SubscriptionARN) {
				return SubscriptionIdentity{SubscriptionARN: item.SubscriptionARN, TopicARN: item.TopicARN, Protocol: item.Protocol, Endpoint: item.Endpoint}, true
			}
		}
		if page.NextToken == "" {
			return SubscriptionIdentity{}, false
		}
		nextToken = page.NextToken
	}
	return SubscriptionIdentity{}, false
}

func validSubscriptionEndpoint(input SubscribeEndpointInput) bool {
	if !topicARNPattern.MatchString(input.TopicARN) || len(input.Endpoint) > 2048 || strings.ContainsAny(input.Endpoint, "\r\n") {
		return false
	}
	switch input.Protocol {
	case "https":
		endpoint, err := url.Parse(input.Endpoint)
		return err == nil && endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == ""
	case "email":
		return subscriptionEmailPattern.MatchString(input.Endpoint)
	case "sqs":
		return subscriptionTargetARNPattern.MatchString(input.Endpoint) && strings.Contains(input.Endpoint, ":sqs:")
	case "lambda":
		return subscriptionTargetARNPattern.MatchString(input.Endpoint) && strings.Contains(input.Endpoint, ":lambda:")
	}
	return false
}

type snsSubscribeSend struct {
	client   *Client
	call     sdkgo.Call
	query    url.Values
	response providerResponse
}

func (send *snsSubscribeSend) dispatch(requestContext context.Context) error {
	send.response = send.client.requestWithContext(send.call, requestContext, send.query)
	return nil
}
