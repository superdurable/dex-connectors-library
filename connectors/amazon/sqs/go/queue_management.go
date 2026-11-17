// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListQueuesInput selects a bounded page of queue URLs in the connection Region.
type ListQueuesInput struct {
	// QueueNamePrefix optionally filters names by their first characters.
	QueueNamePrefix string `json:"queueNamePrefix,omitempty"`
	// MaxResults is 1–1000; zero uses 100.
	MaxResults int `json:"maxResults,omitempty"`
	// NextToken selects the next page; blank starts at the beginning.
	NextToken string `json:"nextToken,omitempty"`
}

// QueuePage contains one page of queue URLs.
type QueuePage struct {
	// QueueURLs are full URLs for queues in the connection Region.
	QueueURLs []string `json:"queueUrls"`
	// NextToken is blank after the final page.
	NextToken string `json:"nextToken,omitempty"`
}

// QueueNameInput identifies one queue by name.
type QueueNameInput struct {
	// QueueName is a standard name or a name ending in .fifo.
	QueueName string `json:"queueName"`
}

// CreateQueueInput defines a queue by name and bounded creation attributes.
type CreateQueueInput struct {
	// QueueName is unique within the account and Region.
	QueueName string `json:"queueName"`
	// Attributes contains FifoQueue, VisibilityTimeout, ReceiveMessageWaitTimeSeconds, DelaySeconds, or MaximumMessageSize.
	Attributes map[string]string `json:"attributes,omitempty"`
}

// QueueURLInput identifies an existing queue; blank uses the connection defaultQueueUrl.
type QueueURLInput struct {
	// QueueURL is the complete SQS queue URL.
	QueueURL string `json:"queueUrl,omitempty"`
}

// QueueIdentity is the complete queue URL returned by SQS or used for a queue mutation.
type QueueIdentity struct {
	// QueueURL is the complete SQS queue URL.
	QueueURL string `json:"queueUrl"`
}

// QueueAttributes contains all settings returned for a queue.
type QueueAttributes struct {
	// QueueURL is the requested queue.
	QueueURL string `json:"queueUrl"`
	// Attributes are provider named settings and approximate queue counts.
	Attributes map[string]string `json:"attributes"`
}

// SetQueueAttributeInput replaces one bounded queue setting.
type SetQueueAttributeInput struct {
	// QueueURL is the target queue; blank uses defaultQueueUrl.
	QueueURL string `json:"queueUrl,omitempty"`
	// AttributeName is VisibilityTimeout, ReceiveMessageWaitTimeSeconds, DelaySeconds, or MaximumMessageSize.
	AttributeName string `json:"attributeName"`
	// AttributeValue is an integer as decimal text, in seconds or bytes according to AttributeName.
	AttributeValue string `json:"attributeValue"`
}

// ListQueuesOperation lists queues.
type ListQueuesOperation struct{ client *Client }

// CreateQueueOperation creates a queue or returns its existing URL.
type CreateQueueOperation struct{ client *Client }

// GetQueueURLOperation finds a queue URL by name.
type GetQueueURLOperation struct{ client *Client }

// GetQueueAttributesOperation reads all queue attributes.
type GetQueueAttributesOperation struct{ client *Client }

// SetQueueAttributeOperation replaces one queue setting.
type SetQueueAttributeOperation struct{ client *Client }

// DeleteQueueOperation deletes one queue.
type DeleteQueueOperation struct{ client *Client }

// ListQueues binds the Query to this client.
func (client *Client) ListQueues() ListQueuesOperation { return ListQueuesOperation{client} }

// CreateQueue binds the Mutation to this client.
func (client *Client) CreateQueue() CreateQueueOperation { return CreateQueueOperation{client} }

// GetQueueURL binds the Query to this client.
func (client *Client) GetQueueURL() GetQueueURLOperation { return GetQueueURLOperation{client} }

// GetQueueAttributes binds the Query to this client.
func (client *Client) GetQueueAttributes() GetQueueAttributesOperation {
	return GetQueueAttributesOperation{client}
}

// SetQueueAttribute binds the Mutation to this client.
func (client *Client) SetQueueAttribute() SetQueueAttributeOperation {
	return SetQueueAttributeOperation{client}
}

// DeleteQueue binds the Mutation to this client.
func (client *Client) DeleteQueue() DeleteQueueOperation { return DeleteQueueOperation{client} }

// Definition returns the generated Query contract.
func (ListQueuesOperation) Definition() sdkgo.QueryDefinition { return ListQueuesDefinition }

// Definition returns the generated Mutation contract.
func (CreateQueueOperation) Definition() sdkgo.MutationDefinition { return CreateQueueDefinition }

// Definition returns the generated Query contract.
func (GetQueueURLOperation) Definition() sdkgo.QueryDefinition { return GetQueueURLDefinition }

// Definition returns the generated Query contract.
func (GetQueueAttributesOperation) Definition() sdkgo.QueryDefinition {
	return GetQueueAttributesDefinition
}

// Definition returns the generated Mutation contract.
func (SetQueueAttributeOperation) Definition() sdkgo.MutationDefinition {
	return SetQueueAttributeDefinition
}

// Definition returns the generated Mutation contract.
func (DeleteQueueOperation) Definition() sdkgo.MutationDefinition { return DeleteQueueDefinition }

// IdempotencyKey keeps one queue creation's Dex call identity stable.
func (CreateQueueOperation) IdempotencyKey(id sdkgo.CallID, _ CreateQueueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey keeps one absolute replacement's Dex call identity stable.
func (SetQueueAttributeOperation) IdempotencyKey(id sdkgo.CallID, _ SetQueueAttributeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey keeps one deletion's Dex call identity stable.
func (DeleteQueueOperation) IdempotencyKey(id sdkgo.CallID, _ QueueURLInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke lists at most one page of queue URLs.
func (operation ListQueuesOperation) Invoke(call sdkgo.Call, input ListQueuesInput) sdkgo.QueryAttempt[QueuePage] {
	const operationID = "listQueues"
	if input.MaxResults == 0 {
		input.MaxResults = 100
	}
	if input.MaxResults < 1 || input.MaxResults > 1000 || len(input.QueueNamePrefix) > 80 || len(input.NextToken) > 1024 || strings.ContainsAny(input.QueueNamePrefix+input.NextToken, "\r\n") {
		return queueQueryBranch[QueuePage](operationID, ListQueuesBranchDefect, sdkgo.FailureValidation, "SQS queue filter or page token is invalid")
	}
	request := map[string]any{"MaxResults": input.MaxResults}
	if input.QueueNamePrefix != "" {
		request["QueueNamePrefix"] = input.QueueNamePrefix
	}
	if input.NextToken != "" {
		request["NextToken"] = input.NextToken
	}
	body, _, failure := operation.client.queueRequest(call, operationID, "queue list", "ListQueues", request)
	if failure != nil {
		return queueQueryFailure[QueuePage](*failure, ListQueuesBranchProviderRejected, ListQueuesBranchInvalidResponse, ListQueuesBranchDefect, "")
	}
	var document struct {
		QueueURLs []string `json:"QueueUrls"`
		NextToken string   `json:"NextToken"`
	}
	if json.Unmarshal(body, &document) != nil || len(document.QueueURLs) > input.MaxResults || len(document.NextToken) > 1024 {
		return queueQueryBranch[QueuePage](operationID, ListQueuesBranchInvalidResponse, sdkgo.FailureProtocol, "SQS returned invalid queue list")
	}
	for _, queueURL := range document.QueueURLs {
		if _, err := parseQueueURL(queueURL); err != nil {
			return queueQueryBranch[QueuePage](operationID, ListQueuesBranchInvalidResponse, sdkgo.FailureProtocol, "SQS returned invalid queue URL")
		}
	}
	return sdkgo.NewQueryBranch(ListQueuesBranchPage, QueuePage{QueueURLs: document.QueueURLs, NextToken: document.NextToken}, nil, sdkgo.Receipt{})
}

// Invoke creates a queue by stable name and absolute creation attributes.
func (operation CreateQueueOperation) Invoke(call sdkgo.Call, input CreateQueueInput) sdkgo.MutationAttempt[QueueIdentity] {
	const operationID = "createQueue"
	if !validQueueName(input.QueueName) || validateQueueCreationAttributes(input) != nil {
		return queueMutationBranch[QueueIdentity](operationID, CreateQueueBranchDefect, sdkgo.FailureValidation, "SQS queue name or attributes are invalid")
	}
	body, requestID, failure := operation.client.queueRequest(call, operationID, "queue creation", "CreateQueue", map[string]any{"QueueName": input.QueueName, "Attributes": input.Attributes})
	if failure != nil {
		return queueMutationFailure[QueueIdentity](*failure, call, operation.client, CreateQueueBranchProviderRejected, CreateQueueBranchInvalidResponse, CreateQueueBranchDefect, "")
	}
	var document struct {
		QueueURL string `json:"QueueUrl"`
	}
	if json.Unmarshal(body, &document) != nil || !queueURLMatchesName(document.QueueURL, input.QueueName) {
		return queueMutationBranch[QueueIdentity](operationID, CreateQueueBranchInvalidResponse, sdkgo.FailureProtocol, "SQS returned invalid created queue URL")
	}
	value := QueueIdentity{QueueURL: document.QueueURL}
	return sdkgo.NewMutationBranch(CreateQueueBranchCreated, value, nil, operation.client.receipt(call, requestID, document.QueueURL))
}

// Invoke resolves a queue name to its URL.
func (operation GetQueueURLOperation) Invoke(call sdkgo.Call, input QueueNameInput) sdkgo.QueryAttempt[QueueIdentity] {
	const operationID = "getQueueURL"
	if !validQueueName(input.QueueName) {
		return queueQueryBranch[QueueIdentity](operationID, GetQueueURLBranchDefect, sdkgo.FailureValidation, "SQS queue name is invalid")
	}
	body, _, failure := operation.client.queueRequest(call, operationID, "queue lookup", "GetQueueUrl", map[string]any{"QueueName": input.QueueName})
	if failure != nil {
		return queueQueryFailure[QueueIdentity](*failure, GetQueueURLBranchProviderRejected, GetQueueURLBranchInvalidResponse, GetQueueURLBranchDefect, GetQueueURLBranchMissing)
	}
	var document struct {
		QueueURL string `json:"QueueUrl"`
	}
	if json.Unmarshal(body, &document) != nil || !queueURLMatchesName(document.QueueURL, input.QueueName) {
		return queueQueryBranch[QueueIdentity](operationID, GetQueueURLBranchInvalidResponse, sdkgo.FailureProtocol, "SQS returned invalid queue URL")
	}
	return sdkgo.NewQueryBranch(GetQueueURLBranchFound, QueueIdentity{QueueURL: document.QueueURL}, nil, sdkgo.Receipt{})
}

// Invoke reads all attributes of one queue.
func (operation GetQueueAttributesOperation) Invoke(call sdkgo.Call, input QueueURLInput) sdkgo.QueryAttempt[QueueAttributes] {
	const operationID = "getQueueAttributes"
	queue, err := operation.client.resolveQueue(input.QueueURL)
	if err != nil {
		return queueQueryBranch[QueueAttributes](operationID, GetQueueAttributesBranchDefect, sdkgo.FailureValidation, "SQS queue URL is invalid")
	}
	body, _, failure := operation.client.queueRequest(call, operationID, "queue attribute read", "GetQueueAttributes", map[string]any{"QueueUrl": queue.url, "AttributeNames": []string{"All"}})
	if failure != nil {
		return queueQueryFailure[QueueAttributes](*failure, GetQueueAttributesBranchProviderRejected, GetQueueAttributesBranchInvalidResponse, GetQueueAttributesBranchDefect, GetQueueAttributesBranchMissing)
	}
	var document struct {
		Attributes map[string]string `json:"Attributes"`
	}
	if json.Unmarshal(body, &document) != nil || document.Attributes == nil || len(document.Attributes) > 128 {
		return queueQueryBranch[QueueAttributes](operationID, GetQueueAttributesBranchInvalidResponse, sdkgo.FailureProtocol, "SQS returned invalid queue attributes")
	}
	for key, value := range document.Attributes {
		if len(key) == 0 || len(key) > 128 || len(value) > 1<<20 || strings.ContainsAny(key, "\r\n") {
			return queueQueryBranch[QueueAttributes](operationID, GetQueueAttributesBranchInvalidResponse, sdkgo.FailureProtocol, "SQS returned invalid queue attribute")
		}
	}
	return sdkgo.NewQueryBranch(GetQueueAttributesBranchFound, QueueAttributes{QueueURL: queue.url, Attributes: document.Attributes}, nil, sdkgo.Receipt{})
}

// Invoke replaces one bounded queue attribute.
func (operation SetQueueAttributeOperation) Invoke(call sdkgo.Call, input SetQueueAttributeInput) sdkgo.MutationAttempt[QueueIdentity] {
	const operationID = "setQueueAttribute"
	queue, err := operation.client.resolveQueue(input.QueueURL)
	if err != nil || validateQueueSetting(input.AttributeName, input.AttributeValue) != nil {
		return queueMutationBranch[QueueIdentity](operationID, SetQueueAttributeBranchDefect, sdkgo.FailureValidation, "SQS queue URL or setting is invalid")
	}
	_, requestID, failure := operation.client.queueRequest(call, operationID, "queue attribute update", "SetQueueAttributes", map[string]any{"QueueUrl": queue.url, "Attributes": map[string]string{input.AttributeName: input.AttributeValue}})
	if failure != nil {
		return queueMutationFailure[QueueIdentity](*failure, call, operation.client, SetQueueAttributeBranchProviderRejected, "", SetQueueAttributeBranchDefect, queue.url)
	}
	return sdkgo.NewMutationBranch(SetQueueAttributeBranchUpdated, QueueIdentity{QueueURL: queue.url}, nil, operation.client.receipt(call, requestID, queue.url))
}

// Invoke deletes a queue by URL. SQS accepts a repeated deletion.
func (operation DeleteQueueOperation) Invoke(call sdkgo.Call, input QueueURLInput) sdkgo.MutationAttempt[QueueIdentity] {
	const operationID = "deleteQueue"
	queue, err := operation.client.resolveQueue(input.QueueURL)
	if err != nil {
		return queueMutationBranch[QueueIdentity](operationID, DeleteQueueBranchDefect, sdkgo.FailureValidation, "SQS queue URL is invalid")
	}
	_, requestID, failure := operation.client.queueRequest(call, operationID, "queue deletion", "DeleteQueue", map[string]any{"QueueUrl": queue.url})
	if failure != nil {
		return queueMutationFailure[QueueIdentity](*failure, call, operation.client, DeleteQueueBranchProviderRejected, "", DeleteQueueBranchDefect, queue.url)
	}
	return sdkgo.NewMutationBranch(DeleteQueueBranchDeleted, QueueIdentity{QueueURL: queue.url}, nil, operation.client.receipt(call, requestID, queue.url))
}

type queueRequestFailure struct {
	classified        classifiedFailure
	isInvalidResponse bool
	isMissing         bool
}

// queueRequest sends one signed JSON action and bounds its complete response.
func (client *Client) queueRequest(call sdkgo.Call, operationID, subject, action string, request any) ([]byte, string, *queueRequestFailure) {
	session, cancel, authFailure := client.startSession(call, operationID)
	if authFailure != nil {
		return nil, "", &queueRequestFailure{classified: classifiedFailure{outcome: outcomeDefect, failure: *authFailure}}
	}
	defer cancel()
	response, err := client.exchange(session, action, request)
	if err != nil {
		outcome := outcomeRetry
		kind := sdkgo.FailureTransport
		if errors.Is(err, errRequestNotBuilt) {
			outcome, kind = outcomeDefect, sdkgo.FailureLocalDefect
		}
		return nil, "", &queueRequestFailure{classified: classifiedFailure{outcome: outcome, failure: newFailure(operationID, kind, "SQS "+subject+" request failed")}}
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		providerError := client.readErrorResponse(response, session.credentials)
		classified := classifyErrorResponse(operationID, subject, providerError)
		return nil, classified.requestID, &queueRequestFailure{classified: classified, isMissing: isQueueMissingErrorCode(providerError.code)}
	}
	requestID := safeRequestID(response.Header)
	body, readErr := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if readErr != nil {
		if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
			return nil, requestID, &queueRequestFailure{classified: classifiedFailure{outcome: outcomeRejected, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "SQS "+subject+" response was too large")}, isInvalidResponse: true}
		}
		return nil, requestID, &queueRequestFailure{classified: classifiedFailure{outcome: outcomeRetry, failure: transportFailure(operationID, subject)}}
	}
	if containsCredential(string(body), session.credentials) {
		return nil, requestID, &queueRequestFailure{classified: classifiedFailure{outcome: outcomeRejected, failure: newFailure(operationID, sdkgo.FailureProtocol, "SQS response contained connection credentials")}, isInvalidResponse: true}
	}
	return body, requestID, nil
}

func queueQueryFailure[T any](failure queueRequestFailure, rejected, invalid, defect, missing sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	if failure.classified.outcome == outcomeRetry {
		return sdkgo.NewQueryRetry[T](failure.classified.failure, failure.classified.retryAfter)
	}
	branch := rejected
	if failure.isMissing && missing != "" {
		branch = missing
	}
	if failure.isInvalidResponse {
		branch = invalid
	}
	if failure.classified.outcome == outcomeDefect {
		branch = defect
	}
	if branch == missing {
		return sdkgo.NewQueryBranch(branch, *new(T), nil, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(branch, *new(T), &failure.classified.failure, sdkgo.Receipt{})
}

func queueMutationFailure[T any](failure queueRequestFailure, call sdkgo.Call, client *Client, rejected, invalid, defect sdkgo.BranchID, objectID string) sdkgo.MutationAttempt[T] {
	if failure.classified.outcome == outcomeRetry {
		return sdkgo.NewMutationRetry[T](failure.classified.failure, failure.classified.retryAfter)
	}
	branch := rejected
	if failure.isInvalidResponse && invalid != "" {
		branch = invalid
	}
	if failure.classified.outcome == outcomeDefect {
		branch = defect
	}
	receipt := client.receipt(call, failure.classified.requestID, objectID)
	return sdkgo.NewMutationBranch(branch, *new(T), &failure.classified.failure, receipt)
}

func queueQueryBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := newFailure(operationID, kind, message)
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func queueMutationBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := newFailure(operationID, kind, message)
	return sdkgo.NewMutationBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func validQueueName(name string) bool {
	baseName, _ := strings.CutSuffix(name, fifoQueueSuffix)
	return len(name) <= 80 && queueNamePattern.MatchString(baseName)
}

func queueURLMatchesName(queueURL, name string) bool {
	queue, err := parseQueueURL(queueURL)
	return err == nil && strings.HasSuffix(queue.url, "/"+name)
}

func validateQueueCreationAttributes(input CreateQueueInput) error {
	if len(input.Attributes) > 6 {
		return errors.New("too many queue attributes")
	}
	isFIFO := strings.HasSuffix(input.QueueName, ".fifo")
	if input.Attributes["FifoQueue"] == "true" && !isFIFO {
		return errors.New("FIFO name must end in .fifo")
	}
	if isFIFO && input.Attributes["FifoQueue"] != "true" {
		return errors.New("FIFO queue requires FifoQueue true")
	}
	for key, value := range input.Attributes {
		if key == "FifoQueue" && value == "true" {
			continue
		}
		if err := validateQueueSetting(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateQueueSetting(key, value string) error {
	lower, upper := 0, 0
	switch key {
	case "VisibilityTimeout":
		lower, upper = 0, 43200
	case "ReceiveMessageWaitTimeSeconds":
		lower, upper = 0, 20
	case "DelaySeconds":
		lower, upper = 0, 900
	case "MaximumMessageSize":
		lower, upper = 1024, 1048576
	default:
		return errors.New("queue attribute is unsupported")
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < lower || number > upper || fmt.Sprint(number) != value {
		return errors.New("queue attribute value is outside its range")
	}
	return nil
}
