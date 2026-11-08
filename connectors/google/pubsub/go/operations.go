// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pubsub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListTopicsInput selects one bounded page of topics in a Google Cloud project.
type ListTopicsInput struct {
	// ProjectID is the Google Cloud project ID shown in Pub/Sub Topics.
	ProjectID string `json:"projectId"`
	// PageSize limits this page to 1 through 100 topics; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is returned by the previous page; blank selects the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// Topic identifies a Pub/Sub topic by its full resource name.
type Topic struct {
	// Name has the form projects/{project-id}/topics/{topic-id}.
	Name string `json:"name"`
}

// TopicsPage holds one page of topic names and its next cursor.
type TopicsPage struct {
	// Topics are visible to the authorized Google identity.
	Topics []Topic `json:"topics"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// ListTopicsOperation reads accessible topics in one project.
type ListTopicsOperation struct{ client *Client }

// ListTopics binds the topic Query to this client.
func (client *Client) ListTopics() ListTopicsOperation { return ListTopicsOperation{client} }

// Definition returns the manifest-generated topic listing contract.
func (ListTopicsOperation) Definition() sdkgo.QueryDefinition { return ListTopicsDefinition }

// Invoke requests a bounded page and projects topic names.
func (operation ListTopicsOperation) Invoke(call sdkgo.Call, input ListTopicsInput) sdkgo.QueryAttempt[TopicsPage] {
	if !validProjectID(input.ProjectID) || input.PageSize < 0 || input.PageSize > 100 || !validPageToken(input.PageToken) {
		return topicBranch(ListTopicsBranchDefect, sdkgo.FailureValidation, "project ID, page size, or page token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	query := url.Values{"pageSize": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	path := "/v1/projects/" + url.PathEscape(input.ProjectID) + "/topics?" + query.Encode()
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return topicBranch(ListTopicsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return topicRetry(sdkgo.FailureTransport, "Pub/Sub request failed", 0)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return topicBranch(ListTopicsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Pub/Sub response exceeded the configured limit")
	}
	if response.err != nil {
		return topicRetry(sdkgo.FailureTransport, "Pub/Sub response was interrupted", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return topicRetry(sdkgo.FailureRateLimit, "Pub/Sub rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return topicRetry(sdkgo.FailureAvailability, "Pub/Sub is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return topicBranch(ListTopicsBranchProviderRejected, sdkgo.FailureProviderRejection, "Pub/Sub rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeTopicsPage(response.body, input)
	if err != nil {
		return topicBranch(ListTopicsBranchInvalidResponse, sdkgo.FailureProtocol, "Pub/Sub returned invalid topic JSON")
	}
	return sdkgo.NewQueryBranch(ListTopicsBranchListed, page, nil, sdkgo.Receipt{})
}

var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._~-]{0,255}$`)

func validProjectID(projectID string) bool { return projectIDPattern.MatchString(projectID) }
func validPageToken(pageToken string) bool {
	return len(pageToken) <= 4096 && !strings.ContainsAny(pageToken, "\r\n")
}

func decodeTopicsPage(body []byte, input ListTopicsInput) (TopicsPage, error) {
	var payload struct {
		Topics []struct {
			Name string `json:"name"`
		} `json:"topics"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return TopicsPage{}, err
	}
	if len(payload.Topics) > input.PageSize || !validPageToken(payload.NextPageToken) {
		return TopicsPage{}, fmt.Errorf("Pub/Sub topic page is invalid")
	}
	page := TopicsPage{Topics: make([]Topic, 0, len(payload.Topics)), NextPageToken: payload.NextPageToken}
	prefix := "projects/" + input.ProjectID + "/topics/"
	for _, item := range payload.Topics {
		topicID := strings.TrimPrefix(item.Name, prefix)
		if topicID == item.Name || topicID == "" || len(topicID) > 255 || strings.ContainsAny(topicID, "/\r\n") {
			return TopicsPage{}, fmt.Errorf("Pub/Sub topic name is invalid")
		}
		page.Topics = append(page.Topics, Topic{Name: item.Name})
	}
	return page, nil
}

func topicBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TopicsPage] {
	failure := topicFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TopicsPage{}, &failure, sdkgo.Receipt{})
}

func topicRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[TopicsPage] {
	return sdkgo.NewQueryRetry[TopicsPage](topicFailure(kind, message), delay)
}

func topicFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "google-pubsub", Operation: "listTopics", Message: message}
}
