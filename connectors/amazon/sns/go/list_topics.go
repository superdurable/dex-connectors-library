// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"encoding/xml"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var nextTokenPattern = regexp.MustCompile(`^[A-Za-z0-9+/=._-]*$`)

// ListTopicsInput requests the first page when NextToken is empty.
type ListTopicsInput struct {
	NextToken string `json:"nextToken"`
}

// TopicPage contains up to 100 topic ARNs and a continuation token.
type TopicPage struct {
	TopicARNs []string `json:"topicARNs"`
	NextToken string   `json:"nextToken"`
}

// ListTopicsOperation reads one page of regional SNS topics.
type ListTopicsOperation struct{ client *Client }

// ListTopics binds the Query to this client.
func (client *Client) ListTopics() ListTopicsOperation { return ListTopicsOperation{client} }

// Definition returns the generated Query contract.
func (ListTopicsOperation) Definition() sdkgo.QueryDefinition { return ListTopicsDefinition }

// Invoke sends one signed Query API request and classifies the outcome.
func (operation ListTopicsOperation) Invoke(call sdkgo.Call, input ListTopicsInput) sdkgo.QueryAttempt[TopicPage] {
	if len(input.NextToken) > 4096 || !nextTokenPattern.MatchString(input.NextToken) {
		return topicBranch(ListTopicsBranchDefect, sdkgo.FailureValidation, "SNS pagination token is invalid")
	}
	response := operation.client.listTopics(call, input.NextToken)
	if errors.Is(response.err, errCredential) {
		return topicBranch(ListTopicsBranchDefect, sdkgo.FailureAuthentication, "SNS credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TopicPage](topicFailure(sdkgo.FailureTransport, "SNS request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TopicPage](topicFailure(sdkgo.FailureRateLimit, "SNS rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TopicPage](topicFailure(sdkgo.FailureAvailability, "SNS is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return topicBranch(ListTopicsBranchProviderRejected, sdkgo.FailureProviderRejection, "SNS rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return topicBranch(ListTopicsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "SNS response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TopicPage](topicFailure(sdkgo.FailureTransport, "SNS response was interrupted"), 0)
	}
	var wire struct {
		XMLName xml.Name `xml:"ListTopicsResponse"`
		Result  *struct {
			Topics []struct {
				ARN string `xml:"TopicArn"`
			} `xml:"Topics>member"`
			NextToken string `xml:"NextToken"`
		} `xml:"ListTopicsResult"`
	}
	if err := xml.Unmarshal(response.body, &wire); err != nil || wire.XMLName.Local != "ListTopicsResponse" || (wire.XMLName.Space != "https://sns.amazonaws.com/doc/2010-03-31/" && wire.XMLName.Space != "http://sns.amazonaws.com/doc/2010-03-31/") || wire.Result == nil || len(wire.Result.Topics) > 100 || len(wire.Result.NextToken) > 4096 || !nextTokenPattern.MatchString(wire.Result.NextToken) {
		return topicBranch(ListTopicsBranchInvalidResponse, sdkgo.FailureProtocol, "SNS returned invalid topic XML")
	}
	page := TopicPage{TopicARNs: make([]string, 0, len(wire.Result.Topics)), NextToken: wire.Result.NextToken}
	for _, topic := range wire.Result.Topics {
		if topic.ARN == "" {
			return topicBranch(ListTopicsBranchInvalidResponse, sdkgo.FailureProtocol, "SNS returned invalid topic XML")
		}
		page.TopicARNs = append(page.TopicARNs, topic.ARN)
	}
	return sdkgo.NewQueryBranch(ListTopicsBranchPage, page, nil, sdkgo.Receipt{})
}

func topicFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amazon-sns", Operation: "listTopics", Message: message}
}
func topicBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TopicPage] {
	failure := topicFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TopicPage{}, &failure, sdkgo.Receipt{})
}
