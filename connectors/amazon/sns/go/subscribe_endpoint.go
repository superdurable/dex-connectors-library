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
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureAvailability, "SNS subscription checkpoint is unavailable"), 0)
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(SubscriptionIdentity{}, topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription could not be confirmed after the send cap"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureAvailability, "SNS unsent checkpoint could not be recorded"), 0)
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureAuthentication, "SNS credentials are unavailable after an earlier send"), 0)
		}
		return subscriptionMutationBranch(operationID, SubscribeEndpointBranchDefect, sdkgo.FailureAuthentication, "SNS credentials are unavailable")
	}
	query := url.Values{"Action": {"Subscribe"}, "Version": {"2010-03-31"}, "TopicArn": {input.TopicARN}, "Protocol": {input.Protocol}, "Endpoint": {input.Endpoint}, "ReturnSubscriptionArn": {"true"}}
	send := &snsSubscribeSend{client: operation.client, call: call, query: query}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureAvailability, "SNS subscription dispatch checkpoint is unavailable"), 0)
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureAvailability, "SNS resend checkpoint could not be recorded"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, kind, "SNS asked the connector to retry subscription"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return limitedSNSUnknown[SubscriptionIdentity](call, decision, topicOperationFailure(operationID, sdkgo.FailureProtocol, "SNS subscription response repeated a credential"))
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return limitedSNSUnknown[SubscriptionIdentity](call, decision, topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription outcome is unknown"))
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends == 0 && isDefiniteSNSRefusal(response.status, response.body) {
			if fence.RecordCanResend() != nil {
				return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureAvailability, "SNS refusal checkpoint is unavailable"), 0)
			}
			return subscriptionMutationBranch(operationID, SubscribeEndpointBranchProviderRejected, sdkgo.FailureProviderRejection, "SNS rejected the subscription")
		}
		return limitedSNSUnknown[SubscriptionIdentity](call, decision, topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription response is inconclusive"))
	}
	var wire struct {
		XMLName xml.Name `xml:"SubscribeResponse"`
		Result  struct {
			ARN string `xml:"SubscriptionArn"`
		} `xml:"SubscribeResult"`
	}
	if xml.Unmarshal(response.body, &wire) != nil || wire.XMLName.Local != "SubscribeResponse" || !isSNSNamespace(wire.XMLName.Space) || !validateSubscriptionARN(wire.Result.ARN) {
		return limitedSNSUnknown[SubscriptionIdentity](call, decision, topicOperationFailure(operationID, sdkgo.FailureProtocol, "SNS subscription response has no usable ARN"))
	}
	value := SubscriptionIdentity{SubscriptionARN: wire.Result.ARN, TopicARN: input.TopicARN, Protocol: input.Protocol, Endpoint: input.Endpoint}
	return sdkgo.NewMutationBranch(SubscribeEndpointBranchSubscribed, value, nil, topicReceipt(call, wire.Result.ARN))
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
