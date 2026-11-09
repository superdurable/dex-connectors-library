// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dynamodb

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
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

// GetItemOperation reads one item from a DynamoDB table.
type GetItemOperation struct{ client *Client }

// GetItem binds the Query to this client.
func (client *Client) GetItem() GetItemOperation { return GetItemOperation{client} }

// Definition returns the generated Query contract.
func (GetItemOperation) Definition() sdkgo.QueryDefinition { return GetItemDefinition }

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
	response := operation.client.getItem(call, payload)
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
