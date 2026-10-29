// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package algolia provides typed index search and record replacement operations.
package algolia

import (
	"bytes"
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

const publicHostTemplate = "https://algolia.net/1"

var applicationIDPattern = regexp.MustCompile(`^[A-Z0-9]+$`)

// Option configures a client dependency at construction.
type Option interface{ applyClientOption(*Client) }

// Client holds immutable HTTP settings and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// SearchInput selects an index and search phrase.
type SearchInput struct {
	// IndexName names the Algolia index.
	IndexName string `json:"indexName"`
	// Query is the search phrase; blank matches all records.
	Query string `json:"query"`
	// HitsPerPage bounds the returned hits; zero uses Algolia's default.
	HitsPerPage int `json:"hitsPerPage,omitempty"`
}

// SearchHit retains only identity and display name from a matching record.
type SearchHit struct {
	// ObjectID is the record's stable ID.
	ObjectID string `json:"objectID"`
	// Name is an optional record name.
	Name string `json:"name,omitempty"`
}

// SearchPage contains a bounded Algolia hit page.
type SearchPage struct {
	// NbHits is Algolia's count of matching records.
	NbHits int `json:"nbHits"`
	// Hits contains the returned record identities.
	Hits []SearchHit `json:"hits"`
}

// SaveObjectInput replaces a record's complete set of string attributes.
type SaveObjectInput struct {
	// IndexName names the target index.
	IndexName string `json:"indexName"`
	// ObjectID identifies the record to replace on retries.
	ObjectID string `json:"objectID"`
	// Attributes are absolute non-secret record values; omitted old attributes are removed.
	Attributes map[string]string `json:"attributes"`
}

// SavedObject confirms Algolia queued an indexing task.
type SavedObject struct {
	// ObjectID identifies the replaced record.
	ObjectID string `json:"objectID"`
	// TaskID identifies the queued Algolia indexing task.
	TaskID int64 `json:"taskID"`
}

// SearchOperation searches one index.
type SearchOperation struct{ client *Client }

// SaveObjectOperation replaces a record by object ID.
type SaveObjectOperation struct{ client *Client }

// New validates settings and constructs a reusable Algolia client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Algolia credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Algolia base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// Search binds the search operation to this client.
func (client *Client) Search() SearchOperation { return SearchOperation{client} }

// SaveObject binds record replacement to this client.
func (client *Client) SaveObject() SaveObjectOperation { return SaveObjectOperation{client} }

// Definition returns the generated search contract.
func (SearchOperation) Definition() sdkgo.QueryDefinition { return SearchDefinition }

// Definition returns the generated record replacement contract.
func (SaveObjectOperation) Definition() sdkgo.MutationDefinition { return SaveObjectDefinition }

// IdempotencyKey uses Dex's call identity; a stable object ID and absolute replacement make retries safe.
func (SaveObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ SaveObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke searches an Algolia index through its public REST API.
func (operation SearchOperation) Invoke(call sdkgo.Call, input SearchInput) sdkgo.QueryAttempt[SearchPage] {
	const name = "search"
	if !validPathPart(input.IndexName) || input.HitsPerPage < 0 || input.HitsPerPage > 1000 {
		return sdkgo.NewQueryBranch(SearchBranchDefect, SearchPage{}, algoliaFailure(name, sdkgo.FailureValidation, "indexName or hitsPerPage is invalid"), sdkgo.Receipt{})
	}
	parameters := url.Values{"query": {input.Query}}
	if input.HitsPerPage > 0 {
		parameters.Set("hitsPerPage", strconv.Itoa(input.HitsPerPage))
	}
	payload, _ := json.Marshal(map[string]string{"params": parameters.Encode()})
	body, status, failure, retryAfter := operation.client.send(call, http.MethodPost, "/indexes/"+url.PathEscape(input.IndexName)+"/query", payload, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewQueryBranch(SearchBranchDefect, SearchPage{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewQueryRetry[SearchPage](*failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(SearchBranchInvalidResponse, SearchPage{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(SearchBranchProviderRejected, SearchPage{}, algoliaFailure(name, rejectionKind(status), "Algolia rejected search with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var page SearchPage
	if json.Unmarshal(body, &page) != nil || page.NbHits < 0 || page.Hits == nil {
		return sdkgo.NewQueryBranch(SearchBranchInvalidResponse, SearchPage{}, algoliaFailure(name, sdkgo.FailureProtocol, "Algolia returned an invalid search page"), sdkgo.Receipt{})
	}
	for _, hit := range page.Hits {
		if hit.ObjectID == "" {
			return sdkgo.NewQueryBranch(SearchBranchInvalidResponse, SearchPage{}, algoliaFailure(name, sdkgo.FailureProtocol, "Algolia returned a hit without objectID"), sdkgo.Receipt{})
		}
	}
	return sdkgo.NewQueryBranch(SearchBranchSearched, page, nil, sdkgo.Receipt{})
}

// Invoke replaces a record by objectID and confirms its queued indexing task.
func (operation SaveObjectOperation) Invoke(call sdkgo.Call, input SaveObjectInput) sdkgo.MutationAttempt[SavedObject] {
	const name = "saveObject"
	if !validPathPart(input.IndexName) || !validPathPart(input.ObjectID) || input.Attributes == nil {
		return sdkgo.NewMutationBranch(SaveObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "indexName, objectID, and attributes are required"), sdkgo.Receipt{})
	}
	if _, reserved := input.Attributes["objectID"]; reserved {
		return sdkgo.NewMutationBranch(SaveObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "objectID belongs in its dedicated field"), sdkgo.Receipt{})
	}
	payloadFields := make(map[string]string, len(input.Attributes)+1)
	for key, value := range input.Attributes {
		if strings.TrimSpace(key) == "" {
			return sdkgo.NewMutationBranch(SaveObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "attribute names must be nonblank"), sdkgo.Receipt{})
		}
		payloadFields[key] = value
	}
	payloadFields["objectID"] = input.ObjectID
	payload, err := json.Marshal(payloadFields)
	if err != nil || len(payload) > 1<<20 {
		return sdkgo.NewMutationBranch(SaveObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "record payload is invalid or too large"), sdkgo.Receipt{})
	}
	body, status, failure, retryAfter := operation.client.send(call, http.MethodPut, "/indexes/"+url.PathEscape(input.IndexName)+"/"+url.PathEscape(input.ObjectID), payload, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewMutationBranch(SaveObjectBranchDefect, SavedObject{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewMutationRetry[SavedObject](*failure, retryAfter)
		}
		return sdkgo.NewMutationBranch(SaveObjectBranchInvalidResponse, SavedObject{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewMutationBranch(SaveObjectBranchProviderRejected, SavedObject{}, algoliaFailure(name, rejectionKind(status), "Algolia rejected record replacement with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var result SavedObject
	if json.Unmarshal(body, &result) != nil || result.ObjectID != input.ObjectID || result.TaskID <= 0 {
		return sdkgo.NewMutationBranch(SaveObjectBranchInvalidResponse, SavedObject{}, algoliaFailure(name, sdkgo.FailureProtocol, "Algolia returned an invalid indexing task"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(SaveObjectBranchSaved, result, nil, sdkgo.Receipt{})
}

func (client *Client) send(call sdkgo.Call, method, path string, payload []byte, operation string) ([]byte, int, *sdkgo.Failure, time.Duration) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !applicationIDPattern.MatchString(credentials.ApplicationID) || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, 0, algoliaFailure(operation, sdkgo.FailureAuthentication, "Algolia credentials are unavailable or invalid"), 0
	}
	baseURL := client.baseURL
	if baseURL == publicHostTemplate {
		baseURL = "https://" + credentials.ApplicationID + ".algolia.net/1"
	}
	request, err := http.NewRequestWithContext(call.Context, method, baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, algoliaFailure(operation, sdkgo.FailureLocalDefect, "Algolia request could not be built"), 0
	}
	request.Header.Set("x-algolia-api-key", credentials.APIKey.Reveal())
	request.Header.Set("x-algolia-application-id", credentials.ApplicationID)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, -1, algoliaFailure(operation, sdkgo.FailureTransport, "Algolia response is unavailable"), 0
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, -1, algoliaFailure(operation, sdkgo.FailureAvailability, "Algolia is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	}
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, nil, 0
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return nil, 1, algoliaFailure(operation, sdkgo.FailureResponseTooLarge, "Algolia response exceeded the limit"), 0
	}
	if err != nil {
		return nil, -1, algoliaFailure(operation, sdkgo.FailureAvailability, "Algolia response was interrupted"), 0
	}
	return body, response.StatusCode, nil, 0
}

func validPathPart(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "/?&#")
}
func rejectionKind(status int) sdkgo.FailureKind {
	if status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}
func algoliaFailure(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "algolia", Operation: operation, Message: message}
}
