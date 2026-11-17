// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var subscriptionARNPattern = regexp.MustCompile(`^arn:aws(-us-gov|-cn)?:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}(\.fifo)?:[A-Za-z0-9-]+$`)

// SubscriptionIdentity identifies one SNS subscription.
type SubscriptionIdentity struct {
	// SubscriptionARN is the ARN returned by SNS, including while confirmation is pending.
	SubscriptionARN string `json:"subscriptionARN"`
	// TopicARN is the subscribed topic when known.
	TopicARN string `json:"topicARN,omitempty"`
	// Protocol is the delivery protocol when known.
	Protocol string `json:"protocol,omitempty"`
	// Endpoint is the notification destination when known.
	Endpoint string `json:"endpoint,omitempty"`
}

// SubscriptionSummary is one SNS list entry.
type SubscriptionSummary struct {
	// SubscriptionARN is the subscription ARN or PendingConfirmation.
	SubscriptionARN string `json:"subscriptionARN"`
	// TopicARN is the subscribed topic.
	TopicARN string `json:"topicARN"`
	// Protocol is the delivery protocol.
	Protocol string `json:"protocol"`
	// Endpoint is the notification destination.
	Endpoint string `json:"endpoint"`
	// Owner is the AWS account ID that owns the subscription.
	Owner string `json:"owner"`
}

// SubscriptionPage contains up to 100 entries and a continuation token.
type SubscriptionPage struct {
	// Subscriptions contains the returned entries.
	Subscriptions []SubscriptionSummary `json:"subscriptions"`
	// NextToken continues the listing; blank means the final page.
	NextToken string `json:"nextToken,omitempty"`
}

// ListSubscriptionsByTopicInput selects a topic and one list page.
type ListSubscriptionsByTopicInput struct {
	// TopicARN is the topic ARN from SNS Console > Topics.
	TopicARN string `json:"topicARN"`
	// NextToken resumes a previous page; blank starts at the beginning.
	NextToken string `json:"nextToken,omitempty"`
}

// ListSubscriptionsInput selects one account subscription page.
type ListSubscriptionsInput struct {
	// NextToken resumes a previous page; blank starts at the beginning.
	NextToken string `json:"nextToken,omitempty"`
}

// ListSubscriptionsByTopicOperation reads subscriptions for one topic.
type ListSubscriptionsByTopicOperation struct{ client *Client }

// ListSubscriptionsByTopic binds the Query to this client.
func (client *Client) ListSubscriptionsByTopic() ListSubscriptionsByTopicOperation {
	return ListSubscriptionsByTopicOperation{client}
}

// Definition returns the generated Query contract.
func (ListSubscriptionsByTopicOperation) Definition() sdkgo.QueryDefinition {
	return ListSubscriptionsByTopicDefinition
}

// Invoke returns one bounded topic subscription page.
func (operation ListSubscriptionsByTopicOperation) Invoke(call sdkgo.Call, input ListSubscriptionsByTopicInput) sdkgo.QueryAttempt[SubscriptionPage] {
	const operationID = "listSubscriptionsByTopic"
	if !topicARNPattern.MatchString(input.TopicARN) || !validSubscriptionToken(input.NextToken) {
		return subscriptionQueryBranch[SubscriptionPage](operationID, ListSubscriptionsByTopicBranchDefect, sdkgo.FailureValidation, "SNS topic ARN or page token is invalid")
	}
	query := url.Values{"Action": {"ListSubscriptionsByTopic"}, "Version": {"2010-03-31"}, "TopicArn": {input.TopicARN}}
	if input.NextToken != "" {
		query.Set("NextToken", input.NextToken)
	}
	response := operation.client.request(call, query)
	if attempt, failed := subscriptionQueryFailure[SubscriptionPage](operationID, response, "", ListSubscriptionsByTopicBranchProviderRejected, ListSubscriptionsByTopicBranchInvalidResponse, ListSubscriptionsByTopicBranchDefect); failed {
		return attempt
	}
	page, err := decodeSubscriptionPage(response.body, "ListSubscriptionsByTopicResponse", "ListSubscriptionsByTopicResult")
	if err != nil {
		return subscriptionQueryBranch[SubscriptionPage](operationID, ListSubscriptionsByTopicBranchInvalidResponse, sdkgo.FailureProtocol, err.Error())
	}
	return sdkgo.NewQueryBranch(ListSubscriptionsByTopicBranchPage, page, nil, sdkgo.Receipt{})
}

// ListSubscriptionsOperation reads all account subscriptions.
type ListSubscriptionsOperation struct{ client *Client }

// ListSubscriptions binds the Query to this client.
func (client *Client) ListSubscriptions() ListSubscriptionsOperation {
	return ListSubscriptionsOperation{client}
}

// Definition returns the generated Query contract.
func (ListSubscriptionsOperation) Definition() sdkgo.QueryDefinition {
	return ListSubscriptionsDefinition
}

// Invoke returns one bounded account subscription page.
func (operation ListSubscriptionsOperation) Invoke(call sdkgo.Call, input ListSubscriptionsInput) sdkgo.QueryAttempt[SubscriptionPage] {
	const operationID = "listSubscriptions"
	if !validSubscriptionToken(input.NextToken) {
		return subscriptionQueryBranch[SubscriptionPage](operationID, ListSubscriptionsBranchDefect, sdkgo.FailureValidation, "SNS page token is invalid")
	}
	query := url.Values{"Action": {"ListSubscriptions"}, "Version": {"2010-03-31"}}
	if input.NextToken != "" {
		query.Set("NextToken", input.NextToken)
	}
	response := operation.client.request(call, query)
	if attempt, failed := subscriptionQueryFailure[SubscriptionPage](operationID, response, "", ListSubscriptionsBranchProviderRejected, ListSubscriptionsBranchInvalidResponse, ListSubscriptionsBranchDefect); failed {
		return attempt
	}
	page, err := decodeSubscriptionPage(response.body, "ListSubscriptionsResponse", "ListSubscriptionsResult")
	if err != nil {
		return subscriptionQueryBranch[SubscriptionPage](operationID, ListSubscriptionsBranchInvalidResponse, sdkgo.FailureProtocol, err.Error())
	}
	return sdkgo.NewQueryBranch(ListSubscriptionsBranchPage, page, nil, sdkgo.Receipt{})
}

func decodeSubscriptionPage(body []byte, responseName, resultName string) (SubscriptionPage, error) {
	var wire struct {
		XMLName     xml.Name
		ListByTopic struct {
			NextToken     string                    `xml:"NextToken"`
			Subscriptions []SubscriptionSummaryWire `xml:"Subscriptions>member"`
		} `xml:"ListSubscriptionsByTopicResult"`
		ListAll struct {
			NextToken     string                    `xml:"NextToken"`
			Subscriptions []SubscriptionSummaryWire `xml:"Subscriptions>member"`
		} `xml:"ListSubscriptionsResult"`
	}
	if xml.Unmarshal(body, &wire) != nil || wire.XMLName.Local != responseName || !isSNSNamespace(wire.XMLName.Space) {
		return SubscriptionPage{}, errors.New("SNS returned invalid subscription XML")
	}
	var items []SubscriptionSummaryWire
	var nextToken string
	switch resultName {
	case "ListSubscriptionsByTopicResult":
		items, nextToken = wire.ListByTopic.Subscriptions, wire.ListByTopic.NextToken
	case "ListSubscriptionsResult":
		items, nextToken = wire.ListAll.Subscriptions, wire.ListAll.NextToken
	default:
		return SubscriptionPage{}, errors.New("SNS list result is invalid")
	}
	if len(items) > 100 || !validSubscriptionToken(nextToken) {
		return SubscriptionPage{}, errors.New("SNS returned invalid subscription page")
	}
	page := SubscriptionPage{Subscriptions: make([]SubscriptionSummary, 0, len(items)), NextToken: nextToken}
	for _, item := range items {
		if (item.SubscriptionARN != "PendingConfirmation" && !subscriptionARNPattern.MatchString(item.SubscriptionARN)) || !topicARNPattern.MatchString(item.TopicARN) || item.Endpoint == "" || item.Protocol == "" || len(item.Owner) != 12 {
			return SubscriptionPage{}, errors.New("SNS returned invalid subscription metadata")
		}
		page.Subscriptions = append(page.Subscriptions, SubscriptionSummary{SubscriptionARN: item.SubscriptionARN, TopicARN: item.TopicARN, Protocol: item.Protocol, Endpoint: item.Endpoint, Owner: item.Owner})
	}
	return page, nil
}

type SubscriptionSummaryWire struct {
	SubscriptionARN string `xml:"SubscriptionArn"`
	TopicARN        string `xml:"TopicArn"`
	Protocol        string `xml:"Protocol"`
	Endpoint        string `xml:"Endpoint"`
	Owner           string `xml:"Owner"`
}

func validSubscriptionToken(token string) bool {
	return len(token) <= 4096 && nextTokenPattern.MatchString(token)
}

func subscriptionQueryFailure[T any](operationID string, response providerResponse, missing, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return subscriptionQueryBranch[T](operationID, defect, sdkgo.FailureAuthentication, "SNS credentials are unavailable"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewQueryRetry[T](topicOperationFailure(operationID, kind, "SNS subscription request is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound && missing != "" {
		return sdkgo.NewQueryBranch(missing, *new(T), nil, sdkgo.Receipt{}), true
	}
	if response.status != http.StatusOK {
		return subscriptionQueryBranch[T](operationID, rejected, sdkgo.FailureProviderRejection, "SNS rejected subscription request"), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return subscriptionQueryBranch[T](operationID, invalid, sdkgo.FailureResponseTooLarge, "SNS subscription response was too large"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](topicOperationFailure(operationID, sdkgo.FailureTransport, "SNS subscription response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func subscriptionQueryBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := topicOperationFailure(operationID, kind, message)
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func validateSubscriptionARN(subscriptionARN string) bool {
	return subscriptionARNPattern.MatchString(subscriptionARN) && !strings.ContainsAny(subscriptionARN, "\r\n")
}
