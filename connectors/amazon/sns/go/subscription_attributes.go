// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// SubscriptionARNInput names one SNS subscription.
type SubscriptionARNInput struct {
	// SubscriptionARN is copied from SNS Console > Subscriptions.
	SubscriptionARN string `json:"subscriptionARN"`
}

// SubscriptionAttributes contains non-secret SNS subscription settings.
type SubscriptionAttributes struct {
	// SubscriptionARN is the requested subscription.
	SubscriptionARN string `json:"subscriptionARN"`
	// Attributes contains the provider's named settings.
	Attributes map[string]string `json:"attributes"`
}

// GetSubscriptionAttributesOperation reads SNS subscription settings.
type GetSubscriptionAttributesOperation struct{ client *Client }

// GetSubscriptionAttributes binds the Query to this client.
func (client *Client) GetSubscriptionAttributes() GetSubscriptionAttributesOperation {
	return GetSubscriptionAttributesOperation{client}
}

// Definition returns the generated Query contract.
func (GetSubscriptionAttributesOperation) Definition() sdkgo.QueryDefinition {
	return GetSubscriptionAttributesDefinition
}

// Invoke reads bounded subscription settings without returning credentials.
func (operation GetSubscriptionAttributesOperation) Invoke(call sdkgo.Call, input SubscriptionARNInput) sdkgo.QueryAttempt[SubscriptionAttributes] {
	const operationID = "getSubscriptionAttributes"
	if !validateSubscriptionARN(input.SubscriptionARN) {
		return subscriptionQueryBranch[SubscriptionAttributes](operationID, GetSubscriptionAttributesBranchDefect, sdkgo.FailureValidation, "SNS subscription ARN is invalid")
	}
	response := operation.client.request(call, url.Values{"Action": {"GetSubscriptionAttributes"}, "Version": {"2010-03-31"}, "SubscriptionArn": {input.SubscriptionARN}})
	if attempt, failed := subscriptionQueryFailure[SubscriptionAttributes](operationID, response, GetSubscriptionAttributesBranchMissing, GetSubscriptionAttributesBranchProviderRejected, GetSubscriptionAttributesBranchInvalidResponse, GetSubscriptionAttributesBranchDefect); failed {
		return attempt
	}
	var wire struct {
		XMLName xml.Name `xml:"GetSubscriptionAttributesResponse"`
		Result  *struct {
			Attributes []struct {
				Key   string `xml:"key"`
				Value string `xml:"value"`
			} `xml:"Attributes>entry"`
		} `xml:"GetSubscriptionAttributesResult"`
	}
	if xml.Unmarshal(response.body, &wire) != nil || wire.XMLName.Local != "GetSubscriptionAttributesResponse" || !isSNSNamespace(wire.XMLName.Space) || wire.Result == nil || len(wire.Result.Attributes) > 64 {
		return subscriptionQueryBranch[SubscriptionAttributes](operationID, GetSubscriptionAttributesBranchInvalidResponse, sdkgo.FailureProtocol, "SNS returned invalid subscription attribute XML")
	}
	attributes := make(map[string]string, len(wire.Result.Attributes))
	for _, entry := range wire.Result.Attributes {
		if entry.Key == "" || len(entry.Key) > 128 || len(entry.Value) > 1<<20 || strings.ContainsAny(entry.Key, "\r\n") {
			return subscriptionQueryBranch[SubscriptionAttributes](operationID, GetSubscriptionAttributesBranchInvalidResponse, sdkgo.FailureProtocol, "SNS returned invalid subscription attributes")
		}
		attributes[entry.Key] = entry.Value
	}
	return sdkgo.NewQueryBranch(GetSubscriptionAttributesBranchFound, SubscriptionAttributes{SubscriptionARN: input.SubscriptionARN, Attributes: attributes}, nil, sdkgo.Receipt{})
}

// SetSubscriptionAttributeInput replaces one delivery or filter setting.
type SetSubscriptionAttributeInput struct {
	// SubscriptionARN is copied from SNS Console > Subscriptions.
	SubscriptionARN string `json:"subscriptionARN"`
	// AttributeName is RawMessageDelivery, FilterPolicy, or FilterPolicyScope.
	AttributeName string `json:"attributeName"`
	// AttributeValue is the complete replacement; blank is invalid.
	AttributeValue string `json:"attributeValue"`
}

// SetSubscriptionAttributeOperation replaces one SNS subscription setting.
type SetSubscriptionAttributeOperation struct{ client *Client }

// SetSubscriptionAttribute binds the Mutation to this client.
func (client *Client) SetSubscriptionAttribute() SetSubscriptionAttributeOperation {
	return SetSubscriptionAttributeOperation{client}
}

// Definition returns the generated Mutation contract.
func (SetSubscriptionAttributeOperation) Definition() sdkgo.MutationDefinition {
	return SetSubscriptionAttributeDefinition
}

// IdempotencyKey identifies retries of this absolute replacement.
func (SetSubscriptionAttributeOperation) IdempotencyKey(callID sdkgo.CallID, _ SetSubscriptionAttributeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the selected attribute.
func (operation SetSubscriptionAttributeOperation) Invoke(call sdkgo.Call, input SetSubscriptionAttributeInput) sdkgo.MutationAttempt[SubscriptionIdentity] {
	const operationID = "setSubscriptionAttribute"
	if !validateSubscriptionARN(input.SubscriptionARN) || input.AttributeValue == "" || len(input.AttributeValue) > 1<<20 ||
		(input.AttributeName != "RawMessageDelivery" && input.AttributeName != "FilterPolicy" && input.AttributeName != "FilterPolicyScope") {
		return subscriptionMutationBranch(operationID, SetSubscriptionAttributeBranchDefect, sdkgo.FailureValidation, "SNS subscription ARN, attribute, or value is invalid")
	}
	if input.AttributeName == "RawMessageDelivery" && input.AttributeValue != "true" && input.AttributeValue != "false" {
		return subscriptionMutationBranch(operationID, SetSubscriptionAttributeBranchDefect, sdkgo.FailureValidation, "SNS raw delivery must be true or false")
	}
	if input.AttributeName == "FilterPolicyScope" && input.AttributeValue != "MessageAttributes" && input.AttributeValue != "MessageBody" {
		return subscriptionMutationBranch(operationID, SetSubscriptionAttributeBranchDefect, sdkgo.FailureValidation, "SNS filter scope is invalid")
	}
	response := operation.client.request(call, url.Values{"Action": {"SetSubscriptionAttributes"}, "Version": {"2010-03-31"}, "SubscriptionArn": {input.SubscriptionARN}, "AttributeName": {input.AttributeName}, "AttributeValue": {input.AttributeValue}})
	if attempt, failed := subscriptionMutationFailure(operationID, response, SetSubscriptionAttributeBranchProviderRejected, SetSubscriptionAttributeBranchDefect); failed {
		return attempt
	}
	if !validSubscriptionWriteXML(response.body, "SetSubscriptionAttributesResponse") {
		return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureProtocol, "SNS set-subscription response was invalid"), 0)
	}
	return sdkgo.NewMutationBranch(SetSubscriptionAttributeBranchUpdated, SubscriptionIdentity{SubscriptionARN: input.SubscriptionARN}, nil, topicReceipt(call, input.SubscriptionARN))
}

// UnsubscribeEndpointOperation removes one SNS subscription.
type UnsubscribeEndpointOperation struct{ client *Client }

// UnsubscribeEndpoint binds the Mutation to this client.
func (client *Client) UnsubscribeEndpoint() UnsubscribeEndpointOperation {
	return UnsubscribeEndpointOperation{client}
}

// Definition returns the generated Mutation contract.
func (UnsubscribeEndpointOperation) Definition() sdkgo.MutationDefinition {
	return UnsubscribeEndpointDefinition
}

// IdempotencyKey identifies retries of one deletion.
func (UnsubscribeEndpointOperation) IdempotencyKey(callID sdkgo.CallID, _ SubscriptionARNInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes a subscription; an absent subscription is already removed.
func (operation UnsubscribeEndpointOperation) Invoke(call sdkgo.Call, input SubscriptionARNInput) sdkgo.MutationAttempt[SubscriptionIdentity] {
	const operationID = "unsubscribeEndpoint"
	if !validateSubscriptionARN(input.SubscriptionARN) {
		return subscriptionMutationBranch(operationID, UnsubscribeEndpointBranchDefect, sdkgo.FailureValidation, "SNS subscription ARN is invalid")
	}
	response := operation.client.request(call, url.Values{"Action": {"Unsubscribe"}, "Version": {"2010-03-31"}, "SubscriptionArn": {input.SubscriptionARN}})
	if response.status == http.StatusNotFound && response.err == nil {
		return sdkgo.NewMutationBranch(UnsubscribeEndpointBranchUnsubscribed, SubscriptionIdentity{SubscriptionARN: input.SubscriptionARN}, nil, topicReceipt(call, input.SubscriptionARN))
	}
	if attempt, failed := subscriptionMutationFailure(operationID, response, UnsubscribeEndpointBranchProviderRejected, UnsubscribeEndpointBranchDefect); failed {
		return attempt
	}
	if !validSubscriptionWriteXML(response.body, "UnsubscribeResponse") {
		return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, sdkgo.FailureProtocol, "SNS unsubscribe response was invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UnsubscribeEndpointBranchUnsubscribed, SubscriptionIdentity{SubscriptionARN: input.SubscriptionARN}, nil, topicReceipt(call, input.SubscriptionARN))
}

func validSubscriptionWriteXML(body []byte, responseName string) bool {
	var wire struct{ XMLName xml.Name }
	return xml.Unmarshal(body, &wire) == nil && wire.XMLName.Local == responseName && isSNSNamespace(wire.XMLName.Space)
}

func subscriptionMutationFailure(operationID string, response providerResponse, rejected, defect sdkgo.BranchID) (sdkgo.MutationAttempt[SubscriptionIdentity], bool) {
	if errors.Is(response.err, errCredential) {
		return subscriptionMutationBranch(operationID, defect, sdkgo.FailureAuthentication, "SNS credentials are unavailable"), true
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		kind := sdkgo.FailureTransport
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		if response.status >= 500 {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[SubscriptionIdentity](topicOperationFailure(operationID, kind, "SNS subscription request should be retried"), response.retryAfter), true
	}
	if response.status != http.StatusOK {
		return subscriptionMutationBranch(operationID, rejected, sdkgo.FailureProviderRejection, "SNS rejected subscription request"), true
	}
	return sdkgo.MutationAttempt[SubscriptionIdentity]{}, false
}

func subscriptionMutationBranch(operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SubscriptionIdentity] {
	failure := topicOperationFailure(operationID, kind, message)
	return sdkgo.NewMutationBranch(branch, SubscriptionIdentity{}, &failure, sdkgo.Receipt{})
}
