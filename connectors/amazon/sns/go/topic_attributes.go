// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"encoding/xml"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TopicAttributes contains non-secret settings returned for one SNS topic.
type TopicAttributes struct {
	// TopicARN is the requested SNS topic.
	TopicARN string `json:"topicARN"`
	// Attributes contains the provider's named settings.
	Attributes map[string]string `json:"attributes"`
}

// GetTopicAttributesOperation reads one topic's settings.
type GetTopicAttributesOperation struct{ client *Client }

// GetTopicAttributes binds the Query to this client.
func (client *Client) GetTopicAttributes() GetTopicAttributesOperation {
	return GetTopicAttributesOperation{client}
}

// Definition returns the generated Query contract.
func (GetTopicAttributesOperation) Definition() sdkgo.QueryDefinition {
	return GetTopicAttributesDefinition
}

// Invoke reads bounded SNS topic attributes.
func (operation GetTopicAttributesOperation) Invoke(call sdkgo.Call, input TopicARNInput) sdkgo.QueryAttempt[TopicAttributes] {
	const operationID = "getTopicAttributes"
	if !topicARNPattern.MatchString(input.TopicARN) {
		return subscriptionQueryBranch[TopicAttributes](operationID, GetTopicAttributesBranchDefect, sdkgo.FailureValidation, "SNS topic ARN is invalid")
	}
	response := operation.client.request(call, url.Values{"Action": {"GetTopicAttributes"}, "Version": {"2010-03-31"}, "TopicArn": {input.TopicARN}})
	if attempt, failed := subscriptionQueryFailure[TopicAttributes](operationID, response, GetTopicAttributesBranchMissing, GetTopicAttributesBranchProviderRejected, GetTopicAttributesBranchInvalidResponse, GetTopicAttributesBranchDefect); failed {
		return attempt
	}
	var wire struct {
		XMLName xml.Name `xml:"GetTopicAttributesResponse"`
		Result  *struct {
			Entries []struct {
				Key   string `xml:"key"`
				Value string `xml:"value"`
			} `xml:"Attributes>entry"`
		} `xml:"GetTopicAttributesResult"`
	}
	if xml.Unmarshal(response.body, &wire) != nil || wire.XMLName.Local != "GetTopicAttributesResponse" || !isSNSNamespace(wire.XMLName.Space) || wire.Result == nil || len(wire.Result.Entries) > 128 {
		return subscriptionQueryBranch[TopicAttributes](operationID, GetTopicAttributesBranchInvalidResponse, sdkgo.FailureProtocol, "SNS returned invalid topic attribute XML")
	}
	attributes := make(map[string]string, len(wire.Result.Entries))
	for _, entry := range wire.Result.Entries {
		if entry.Key == "" || len(entry.Key) > 128 || len(entry.Value) > 1<<20 || strings.ContainsAny(entry.Key, "\r\n") {
			return subscriptionQueryBranch[TopicAttributes](operationID, GetTopicAttributesBranchInvalidResponse, sdkgo.FailureProtocol, "SNS returned invalid topic attributes")
		}
		attributes[entry.Key] = entry.Value
	}
	return sdkgo.NewQueryBranch(GetTopicAttributesBranchFound, TopicAttributes{TopicARN: input.TopicARN, Attributes: attributes}, nil, sdkgo.Receipt{})
}
