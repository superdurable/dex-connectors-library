// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var topicNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
var topicARNPattern = regexp.MustCompile(`^arn:aws(-us-gov|-cn)?:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}(\.fifo)?$`)

// CreateTopicInput names a standard topic; an existing owned name returns its ARN.
type CreateTopicInput struct {
	// Name is the console Topics > Create topic name, 1–256 letters, digits, underscores, or hyphens.
	Name string `json:"name"`
}

// TopicARNInput names the topic to delete in the connection's Region.
type TopicARNInput struct {
	// TopicARN is the complete ARN shown in SNS Console > Topics > selected topic.
	TopicARN string `json:"topicARN"`
}

// SetTopicAttributeInput replaces one supported topic attribute.
type SetTopicAttributeInput struct {
	// TopicARN is the complete ARN shown in SNS Console > Topics > selected topic.
	TopicARN string `json:"topicARN"`
	// AttributeName is one of DisplayName, TracingConfig, or MaximumMessageSize.
	AttributeName string `json:"attributeName"`
	// AttributeValue is the full replacement value, not an incremental change.
	AttributeValue string `json:"attributeValue"`
}

// TopicIdentity identifies the topic after a write.
type TopicIdentity struct {
	// TopicARN is the complete regional SNS topic ARN.
	TopicARN string `json:"topicARN"`
}

// CreateTopicOperation implements idempotent topic creation by name.
type CreateTopicOperation struct{ client *Client }

// CreateTopic binds the mutation to this client.
func (client *Client) CreateTopic() CreateTopicOperation { return CreateTopicOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateTopicOperation) Definition() sdkgo.MutationDefinition { return CreateTopicDefinition }

// IdempotencyKey identifies retries of this Step.
func (CreateTopicOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTopicInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the topic or returns the existing ARN for its name.
func (operation CreateTopicOperation) Invoke(call sdkgo.Call, input CreateTopicInput) sdkgo.MutationAttempt[TopicIdentity] {
	if !topicNamePattern.MatchString(input.Name) {
		return topicMutationBranch(CreateTopicBranchDefect, "createTopic", sdkgo.FailureValidation, "SNS topic name is invalid")
	}
	response := operation.client.request(call, url.Values{"Action": {"CreateTopic"}, "Version": {"2010-03-31"}, "Name": {input.Name}})
	if attempt, hasFailure := classifyTopicWriteResponse(response, "createTopic", CreateTopicBranchProviderRejected, CreateTopicBranchDefect); hasFailure {
		return attempt
	}
	var wire struct {
		XMLName xml.Name `xml:"CreateTopicResponse"`
		Result  struct {
			ARN string `xml:"TopicArn"`
		} `xml:"CreateTopicResult"`
	}
	if xml.Unmarshal(response.body, &wire) != nil || wire.XMLName.Local != "CreateTopicResponse" || !isSNSNamespace(wire.XMLName.Space) || !topicARNPattern.MatchString(wire.Result.ARN) {
		return sdkgo.NewMutationRetry[TopicIdentity](topicOperationFailure("createTopic", sdkgo.FailureProtocol, "SNS create-topic response was invalid"), 0)
	}
	value := TopicIdentity{TopicARN: wire.Result.ARN}
	return sdkgo.NewMutationBranch(CreateTopicBranchCreated, value, nil, topicReceipt(call, wire.Result.ARN))
}

// DeleteTopicOperation implements idempotent topic deletion.
type DeleteTopicOperation struct{ client *Client }

// DeleteTopic binds the mutation to this client.
func (client *Client) DeleteTopic() DeleteTopicOperation { return DeleteTopicOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteTopicOperation) Definition() sdkgo.MutationDefinition { return DeleteTopicDefinition }

// IdempotencyKey identifies retries of this Step.
func (DeleteTopicOperation) IdempotencyKey(callID sdkgo.CallID, _ TopicARNInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes the topic and treats a missing topic as already deleted.
func (operation DeleteTopicOperation) Invoke(call sdkgo.Call, input TopicARNInput) sdkgo.MutationAttempt[TopicIdentity] {
	if !topicARNPattern.MatchString(input.TopicARN) {
		return topicMutationBranch(DeleteTopicBranchDefect, "deleteTopic", sdkgo.FailureValidation, "SNS topic ARN is invalid")
	}
	response := operation.client.request(call, url.Values{"Action": {"DeleteTopic"}, "Version": {"2010-03-31"}, "TopicArn": {input.TopicARN}})
	if response.status == http.StatusNotFound && response.err == nil {
		return sdkgo.NewMutationBranch(DeleteTopicBranchDeleted, TopicIdentity{TopicARN: input.TopicARN}, nil, topicReceipt(call, input.TopicARN))
	}
	if attempt, hasFailure := classifyTopicWriteResponse(response, "deleteTopic", DeleteTopicBranchProviderRejected, DeleteTopicBranchDefect); hasFailure {
		return attempt
	}
	var wire struct {
		XMLName xml.Name `xml:"DeleteTopicResponse"`
	}
	if xml.Unmarshal(response.body, &wire) != nil || wire.XMLName.Local != "DeleteTopicResponse" || !isSNSNamespace(wire.XMLName.Space) {
		return sdkgo.NewMutationRetry[TopicIdentity](topicOperationFailure("deleteTopic", sdkgo.FailureProtocol, "SNS delete-topic response was invalid"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteTopicBranchDeleted, TopicIdentity{TopicARN: input.TopicARN}, nil, topicReceipt(call, input.TopicARN))
}

// SetTopicAttributeOperation implements absolute topic attribute replacement.
type SetTopicAttributeOperation struct{ client *Client }

// SetTopicAttribute binds the mutation to this client.
func (client *Client) SetTopicAttribute() SetTopicAttributeOperation {
	return SetTopicAttributeOperation{client}
}

// Definition returns the generated Mutation contract.
func (SetTopicAttributeOperation) Definition() sdkgo.MutationDefinition {
	return SetTopicAttributeDefinition
}

// IdempotencyKey identifies retries of this Step.
func (SetTopicAttributeOperation) IdempotencyKey(callID sdkgo.CallID, _ SetTopicAttributeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces one supported attribute with the given full value.
func (operation SetTopicAttributeOperation) Invoke(call sdkgo.Call, input SetTopicAttributeInput) sdkgo.MutationAttempt[TopicIdentity] {
	if !topicARNPattern.MatchString(input.TopicARN) || input.AttributeValue == "" || len(input.AttributeValue) > 1024 || strings.ContainsAny(input.AttributeValue, "\r\n") ||
		(input.AttributeName != "DisplayName" && input.AttributeName != "TracingConfig" && input.AttributeName != "MaximumMessageSize") {
		return topicMutationBranch(SetTopicAttributeBranchDefect, "setTopicAttribute", sdkgo.FailureValidation, "SNS topic attribute input is invalid")
	}
	if input.AttributeName == "TracingConfig" && input.AttributeValue != "Active" && input.AttributeValue != "PassThrough" {
		return topicMutationBranch(SetTopicAttributeBranchDefect, "setTopicAttribute", sdkgo.FailureValidation, "SNS tracing configuration is invalid")
	}
	if input.AttributeName == "MaximumMessageSize" {
		messageSize, err := strconv.Atoi(input.AttributeValue)
		if err != nil || messageSize < 1024 || messageSize > 1048576 {
			return topicMutationBranch(SetTopicAttributeBranchDefect, "setTopicAttribute", sdkgo.FailureValidation, "SNS maximum message size is invalid")
		}
	}
	response := operation.client.request(call, url.Values{"Action": {"SetTopicAttributes"}, "Version": {"2010-03-31"}, "TopicArn": {input.TopicARN}, "AttributeName": {input.AttributeName}, "AttributeValue": {input.AttributeValue}})
	if attempt, hasFailure := classifyTopicWriteResponse(response, "setTopicAttribute", SetTopicAttributeBranchProviderRejected, SetTopicAttributeBranchDefect); hasFailure {
		return attempt
	}
	var wire struct {
		XMLName xml.Name `xml:"SetTopicAttributesResponse"`
	}
	if xml.Unmarshal(response.body, &wire) != nil || wire.XMLName.Local != "SetTopicAttributesResponse" || !isSNSNamespace(wire.XMLName.Space) {
		return sdkgo.NewMutationRetry[TopicIdentity](topicOperationFailure("setTopicAttribute", sdkgo.FailureProtocol, "SNS set-attribute response was invalid"), 0)
	}
	return sdkgo.NewMutationBranch(SetTopicAttributeBranchUpdated, TopicIdentity{TopicARN: input.TopicARN}, nil, topicReceipt(call, input.TopicARN))
}

func classifyTopicWriteResponse(response providerResponse, operation string, rejected sdkgo.BranchID, defect sdkgo.BranchID) (sdkgo.MutationAttempt[TopicIdentity], bool) {
	if errors.Is(response.err, errCredential) {
		return topicMutationBranch(defect, operation, sdkgo.FailureAuthentication, "SNS credentials are unavailable or invalid"), true
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		kind := sdkgo.FailureTransport
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		if response.status >= 500 {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[TopicIdentity](topicOperationFailure(operation, kind, "SNS topic request should be retried"), response.retryAfter), true
	}
	if response.status != http.StatusOK {
		return topicMutationBranch(rejected, operation, sdkgo.FailureProviderRejection, "SNS rejected the topic request"), true
	}
	return sdkgo.MutationAttempt[TopicIdentity]{}, false
}

func isSNSNamespace(namespace string) bool {
	return namespace == "https://sns.amazonaws.com/doc/2010-03-31/" || namespace == "http://sns.amazonaws.com/doc/2010-03-31/"
}

func topicReceipt(call sdkgo.Call, topicARN string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amazon-sns", ProviderObjectID: topicARN}
}

func topicOperationFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-sns", Operation: operation, Message: message}
}

func topicMutationBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TopicIdentity] {
	failure := topicOperationFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, TopicIdentity{}, &failure, sdkgo.Receipt{})
}
