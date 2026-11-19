// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dynamodb

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var tableNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,255}$`)
var numberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[Ee][+-]?[0-9]+)?$`)

// GetItemInput identifies a table row by all of its S, N, or B key attributes.
type GetItemInput struct {
	TableName      string                     `json:"tableName"`
	Key            map[string]json.RawMessage `json:"key"`
	ConsistentRead bool                       `json:"consistentRead"`
}

// ItemResult returns the item's AttributeValue map when it exists.
type ItemResult struct {
	Exists bool                       `json:"exists"`
	Item   map[string]json.RawMessage `json:"item"`
}

// PutItemInput supplies a complete item including its table primary key.
type PutItemInput struct {
	// TableName identifies the table in the configured Region.
	TableName string `json:"tableName"`
	// Item is the complete map of DynamoDB AttributeValue objects.
	Item map[string]json.RawMessage `json:"item"`
}

// UpdateItemInput sets absolute values on one item by complete primary key.
type UpdateItemInput struct {
	// TableName identifies the table in the configured Region.
	TableName string `json:"tableName"`
	// Key is the complete table primary key.
	Key map[string]json.RawMessage `json:"key"`
	// Attributes contains non-key attributes to set to absolute values.
	Attributes map[string]json.RawMessage `json:"attributes"`
}

// DeleteItemInput identifies one item by its complete primary key.
type DeleteItemInput struct {
	// TableName identifies the table in the configured Region.
	TableName string `json:"tableName"`
	// Key is the complete table primary key.
	Key map[string]json.RawMessage `json:"key"`
}

// ScanItemsInput selects one bounded page of table items.
type ScanItemsInput struct {
	// TableName identifies the table in the configured Region.
	TableName string `json:"tableName"`
	// Limit bounds evaluated items, 1 to 1000; zero uses DynamoDB's page limit.
	Limit int `json:"limit,omitempty"`
	// ExclusiveStartKey resumes from a prior page's LastEvaluatedKey; nil starts at the beginning.
	ExclusiveStartKey map[string]json.RawMessage `json:"exclusiveStartKey,omitempty"`
}

// ItemWriteResult confirms DynamoDB accepted a write without storing item values.
type ItemWriteResult struct {
	// Applied reports that DynamoDB accepted the write.
	Applied bool `json:"applied"`
}

// ItemPage contains one bounded scan page and its continuation key.
type ItemPage struct {
	// Items are the returned DynamoDB AttributeValue maps.
	Items []map[string]json.RawMessage `json:"items"`
	// Count is the number of returned items.
	Count int `json:"count"`
	// LastEvaluatedKey is the continuation key, or empty at the end.
	LastEvaluatedKey map[string]json.RawMessage `json:"lastEvaluatedKey"`
}

// GetItemOperation reads one item from a DynamoDB table.
type GetItemOperation struct{ client *Client }

// PutItemOperation replaces one complete DynamoDB item.
type PutItemOperation struct{ client *Client }

// UpdateItemOperation sets absolute values on one item.
type UpdateItemOperation struct{ client *Client }

// DeleteItemOperation removes one item by primary key.
type DeleteItemOperation struct{ client *Client }

// ScanItemsOperation reads one bounded item page.
type ScanItemsOperation struct{ client *Client }

// GetItem binds the Query to this client.
func (client *Client) GetItem() GetItemOperation { return GetItemOperation{client} }

// PutItem binds a complete item replacement to this client.
func (client *Client) PutItem() PutItemOperation { return PutItemOperation{client} }

// UpdateItem binds an absolute attribute update to this client.
func (client *Client) UpdateItem() UpdateItemOperation { return UpdateItemOperation{client} }

// DeleteItem binds item deletion to this client.
func (client *Client) DeleteItem() DeleteItemOperation { return DeleteItemOperation{client} }

// ScanItems binds bounded table scanning to this client.
func (client *Client) ScanItems() ScanItemsOperation { return ScanItemsOperation{client} }

// Definition returns the generated Query contract.
func (GetItemOperation) Definition() sdkgo.QueryDefinition { return GetItemDefinition }

// Definition returns the generated replacement contract.
func (PutItemOperation) Definition() sdkgo.MutationDefinition { return PutItemDefinition }

// Definition returns the generated attribute update contract.
func (UpdateItemOperation) Definition() sdkgo.MutationDefinition { return UpdateItemDefinition }

// Definition returns the generated deletion contract.
func (DeleteItemOperation) Definition() sdkgo.MutationDefinition { return DeleteItemDefinition }

// Definition returns the generated scan contract.
func (ScanItemsOperation) Definition() sdkgo.QueryDefinition { return ScanItemsDefinition }

// IdempotencyKey records Dex's call identity for a complete replacement.
func (PutItemOperation) IdempotencyKey(callID sdkgo.CallID, _ PutItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records Dex's call identity for an absolute update.
func (UpdateItemOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records Dex's call identity for an idempotent deletion.
func (DeleteItemOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a signed GetItem request and classifies its outcome.
func (operation GetItemOperation) Invoke(call sdkgo.Call, input GetItemInput) sdkgo.QueryAttempt[ItemResult] {
	if !tableNamePattern.MatchString(input.TableName) || !isValidKey(input.Key) {
		return itemBranch(GetItemBranchDefect, sdkgo.FailureValidation, "DynamoDB table or key is invalid")
	}
	payload, err := json.Marshal(struct {
		TableName      string                     `json:"TableName"`
		Key            map[string]json.RawMessage `json:"Key"`
		ConsistentRead bool                       `json:"ConsistentRead"`
	}{input.TableName, input.Key, input.ConsistentRead})
	if err != nil {
		return itemBranch(GetItemBranchDefect, sdkgo.FailureValidation, "DynamoDB key could not be encoded")
	}
	response := operation.client.send(call, "GetItem", payload)
	if errors.Is(response.err, errCredential) {
		return itemBranch(GetItemBranchDefect, sdkgo.FailureAuthentication, "DynamoDB credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ItemResult](itemFailure(sdkgo.FailureTransport, "DynamoDB request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status != http.StatusOK && isThrottled(response.body) {
		return sdkgo.NewQueryRetry[ItemResult](itemFailure(sdkgo.FailureRateLimit, "DynamoDB rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ItemResult](itemFailure(sdkgo.FailureAvailability, "DynamoDB is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return itemBranch(GetItemBranchProviderRejected, sdkgo.FailureProviderRejection, "DynamoDB rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return itemBranch(GetItemBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "DynamoDB response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ItemResult](itemFailure(sdkgo.FailureTransport, "DynamoDB response was interrupted"), 0)
	}
	var wire struct {
		Item map[string]json.RawMessage `json:"Item"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil {
		return itemBranch(GetItemBranchInvalidResponse, sdkgo.FailureProtocol, "DynamoDB returned invalid item JSON")
	}
	if len(wire.Item) == 0 {
		return sdkgo.NewQueryBranch(GetItemBranchMissing, ItemResult{}, nil, sdkgo.Receipt{})
	}
	for name, value := range wire.Item {
		if name == "" || !isAttributeObject(value) {
			return itemBranch(GetItemBranchInvalidResponse, sdkgo.FailureProtocol, "DynamoDB returned invalid item JSON")
		}
	}
	return sdkgo.NewQueryBranch(GetItemBranchFound, ItemResult{Exists: true, Item: wire.Item}, nil, sdkgo.Receipt{})
}

// Invoke replaces a complete DynamoDB item, creating it when absent.
func (operation PutItemOperation) Invoke(call sdkgo.Call, input PutItemInput) sdkgo.MutationAttempt[ItemWriteResult] {
	const name = "putItem"
	branches := writeBranches{written: PutItemBranchWritten, rejected: PutItemBranchProviderRejected, invalid: PutItemBranchInvalidResponse, defect: PutItemBranchDefect}
	if !tableNamePattern.MatchString(input.TableName) || !isValidItem(input.Item) {
		return writeBranch(name, branches.defect, sdkgo.FailureValidation, "DynamoDB table or item is invalid")
	}
	payload, err := json.Marshal(struct {
		TableName string                     `json:"TableName"`
		Item      map[string]json.RawMessage `json:"Item"`
	}{input.TableName, input.Item})
	if err != nil {
		return writeBranch(name, branches.defect, sdkgo.FailureValidation, "DynamoDB item could not be encoded")
	}
	return invokeItemWrite(call, operation.client, name, "PutItem", branches, payload)
}

// Invoke sets absolute non-key attributes, creating the item when absent.
func (operation UpdateItemOperation) Invoke(call sdkgo.Call, input UpdateItemInput) sdkgo.MutationAttempt[ItemWriteResult] {
	const name = "updateItem"
	branches := writeBranches{written: UpdateItemBranchUpdated, rejected: UpdateItemBranchProviderRejected, invalid: UpdateItemBranchInvalidResponse, defect: UpdateItemBranchDefect}
	if !tableNamePattern.MatchString(input.TableName) || !isValidKey(input.Key) || !isValidItem(input.Attributes) {
		return writeBranch(name, branches.defect, sdkgo.FailureValidation, "DynamoDB table, key, or attributes are invalid")
	}
	attributeNames := make([]string, 0, len(input.Attributes))
	for name := range input.Attributes {
		if _, isKey := input.Key[name]; isKey {
			return writeBranch("updateItem", branches.defect, sdkgo.FailureValidation, "DynamoDB key attributes cannot be updated")
		}
		attributeNames = append(attributeNames, name)
	}
	sort.Strings(attributeNames)
	aliases := make(map[string]string, len(attributeNames))
	values := make(map[string]json.RawMessage, len(attributeNames))
	assignments := make([]string, 0, len(attributeNames))
	for index, attributeName := range attributeNames {
		nameAlias := "#a" + strconv.Itoa(index)
		valueAlias := ":v" + strconv.Itoa(index)
		aliases[nameAlias] = attributeName
		values[valueAlias] = input.Attributes[attributeName]
		assignments = append(assignments, nameAlias+" = "+valueAlias)
	}
	payload, err := json.Marshal(struct {
		TableName                 string                     `json:"TableName"`
		Key                       map[string]json.RawMessage `json:"Key"`
		UpdateExpression          string                     `json:"UpdateExpression"`
		ExpressionAttributeNames  map[string]string          `json:"ExpressionAttributeNames"`
		ExpressionAttributeValues map[string]json.RawMessage `json:"ExpressionAttributeValues"`
	}{input.TableName, input.Key, "SET " + strings.Join(assignments, ", "), aliases, values})
	if err != nil {
		return writeBranch(name, branches.defect, sdkgo.FailureValidation, "DynamoDB update could not be encoded")
	}
	return invokeItemWrite(call, operation.client, name, "UpdateItem", branches, payload)
}

// Invoke deletes one item by complete primary key.
func (operation DeleteItemOperation) Invoke(call sdkgo.Call, input DeleteItemInput) sdkgo.MutationAttempt[ItemWriteResult] {
	const name = "deleteItem"
	branches := writeBranches{written: DeleteItemBranchDeleted, rejected: DeleteItemBranchProviderRejected, invalid: DeleteItemBranchInvalidResponse, defect: DeleteItemBranchDefect}
	if !tableNamePattern.MatchString(input.TableName) || !isValidKey(input.Key) {
		return writeBranch(name, branches.defect, sdkgo.FailureValidation, "DynamoDB table or key is invalid")
	}
	payload, err := json.Marshal(struct {
		TableName string                     `json:"TableName"`
		Key       map[string]json.RawMessage `json:"Key"`
	}{input.TableName, input.Key})
	if err != nil {
		return writeBranch(name, branches.defect, sdkgo.FailureValidation, "DynamoDB key could not be encoded")
	}
	return invokeItemWrite(call, operation.client, name, "DeleteItem", branches, payload)
}

// Invoke scans a bounded page and returns its continuation key.
func (operation ScanItemsOperation) Invoke(call sdkgo.Call, input ScanItemsInput) sdkgo.QueryAttempt[ItemPage] {
	const name = "scanItems"
	if !tableNamePattern.MatchString(input.TableName) || input.Limit < 0 || input.Limit > 1000 || input.ExclusiveStartKey != nil && !isValidKey(input.ExclusiveStartKey) {
		return scanBranch(ScanItemsBranchDefect, sdkgo.FailureValidation, "DynamoDB scan input is invalid")
	}
	payload, err := json.Marshal(struct {
		TableName         string                     `json:"TableName"`
		Limit             int                        `json:"Limit,omitempty"`
		ExclusiveStartKey map[string]json.RawMessage `json:"ExclusiveStartKey,omitempty"`
	}{input.TableName, input.Limit, input.ExclusiveStartKey})
	if err != nil {
		return scanBranch(ScanItemsBranchDefect, sdkgo.FailureValidation, "DynamoDB scan input could not be encoded")
	}
	response := operation.client.send(call, "Scan", payload)
	if errors.Is(response.err, errCredential) {
		return scanBranch(ScanItemsBranchDefect, sdkgo.FailureAuthentication, "DynamoDB credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ItemPage](scanFailure(sdkgo.FailureTransport, "DynamoDB request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status != http.StatusOK && isThrottled(response.body) {
		return sdkgo.NewQueryRetry[ItemPage](scanFailure(sdkgo.FailureRateLimit, "DynamoDB rate limited the scan"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ItemPage](scanFailure(sdkgo.FailureAvailability, "DynamoDB is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return scanBranch(ScanItemsBranchProviderRejected, sdkgo.FailureProviderRejection, "DynamoDB rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return scanBranch(ScanItemsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "DynamoDB response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ItemPage](scanFailure(sdkgo.FailureTransport, "DynamoDB response was interrupted"), 0)
	}
	var wire struct {
		Items            []map[string]json.RawMessage `json:"Items"`
		Count            int                          `json:"Count"`
		LastEvaluatedKey map[string]json.RawMessage   `json:"LastEvaluatedKey"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Items == nil || wire.Count < 0 || wire.Count != len(wire.Items) || wire.LastEvaluatedKey != nil && !isValidKey(wire.LastEvaluatedKey) {
		return scanBranch(ScanItemsBranchInvalidResponse, sdkgo.FailureProtocol, "DynamoDB returned invalid scan JSON")
	}
	for _, item := range wire.Items {
		if !isValidItem(item) {
			return scanBranch(ScanItemsBranchInvalidResponse, sdkgo.FailureProtocol, "DynamoDB returned invalid item JSON")
		}
	}
	return sdkgo.NewQueryBranch(ScanItemsBranchScanned, ItemPage{Items: wire.Items, Count: wire.Count, LastEvaluatedKey: wire.LastEvaluatedKey}, nil, sdkgo.Receipt{})
}

type writeBranches struct{ written, rejected, invalid, defect sdkgo.BranchID }

func invokeItemWrite(call sdkgo.Call, client *Client, operation, target string, branches writeBranches, payload []byte) sdkgo.MutationAttempt[ItemWriteResult] {
	response := client.send(call, target, payload)
	if errors.Is(response.err, errCredential) {
		return writeBranch(operation, branches.defect, sdkgo.FailureAuthentication, "DynamoDB credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[ItemWriteResult](writeFailure(operation, sdkgo.FailureTransport, "DynamoDB request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status != http.StatusOK && isThrottled(response.body) {
		return sdkgo.NewMutationRetry[ItemWriteResult](writeFailure(operation, sdkgo.FailureRateLimit, "DynamoDB rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ItemWriteResult](writeFailure(operation, sdkgo.FailureAvailability, "DynamoDB is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return writeBranch(operation, branches.rejected, sdkgo.FailureProviderRejection, "DynamoDB rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return writeBranch(operation, branches.invalid, sdkgo.FailureResponseTooLarge, "DynamoDB response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[ItemWriteResult](writeFailure(operation, sdkgo.FailureTransport, "DynamoDB response was interrupted"), 0)
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(response.body, &body) != nil || body == nil {
		return writeBranch(operation, branches.invalid, sdkgo.FailureProtocol, "DynamoDB returned invalid write JSON")
	}
	return sdkgo.NewMutationBranch(branches.written, ItemWriteResult{Applied: true}, nil, sdkgo.Receipt{})
}

func writeFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-dynamodb", Operation: operation, Message: message}
}

func writeBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ItemWriteResult] {
	failure := writeFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ItemWriteResult{}, &failure, sdkgo.Receipt{})
}

func scanFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-dynamodb", Operation: "scanItems", Message: message}
}

func scanBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ItemPage] {
	failure := scanFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ItemPage{Items: []map[string]json.RawMessage{}}, &failure, sdkgo.Receipt{})
}

func isValidItem(item map[string]json.RawMessage) bool {
	if len(item) < 1 || len(item) > 400 {
		return false
	}
	for name, value := range item {
		if name == "" || len(name) > 255 || strings.ContainsAny(name, "\x00\r\n") || !isAttributeObject(value) {
			return false
		}
	}
	return true
}

func isValidKey(key map[string]json.RawMessage) bool {
	if len(key) < 1 || len(key) > 2 {
		return false
	}
	for name, value := range key {
		if name == "" || len(name) > 255 || strings.ContainsAny(name, "\x00\r\n") || !isKeyAttribute(value) {
			return false
		}
	}
	return true
}
func isKeyAttribute(value json.RawMessage) bool {
	var attribute map[string]json.RawMessage
	if json.Unmarshal(value, &attribute) != nil || len(attribute) != 1 {
		return false
	}
	for kind, raw := range attribute {
		var text string
		if json.Unmarshal(raw, &text) != nil || text == "" {
			return false
		}
		switch kind {
		case "S":
			return true
		case "N":
			return numberPattern.MatchString(text)
		case "B":
			_, err := base64.StdEncoding.DecodeString(text)
			return err == nil
		default:
			return false
		}
	}
	return false
}
func isAttributeObject(value json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && len(object) == 1
}
func isThrottled(body []byte) bool {
	var errorBody struct {
		Type string `json:"__type"`
	}
	if json.Unmarshal(body, &errorBody) != nil {
		return false
	}
	code := errorBody.Type
	if index := strings.LastIndexByte(code, '#'); index >= 0 {
		code = code[index+1:]
	}
	switch code {
	case "ProvisionedThroughputExceededException", "ThrottlingException", "RequestLimitExceeded", "TransactionConflictException":
		return true
	}
	return false
}
func itemFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-dynamodb", Operation: "getItem", Message: message}
}
func itemBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ItemResult] {
	failure := itemFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ItemResult{}, &failure, sdkgo.Receipt{})
}
