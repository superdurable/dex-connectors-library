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
	Query string `json:"query,omitempty"`
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

// ObjectInput identifies one record in an Algolia index.
type ObjectInput struct {
	// IndexName names the existing index.
	IndexName string `json:"indexName"`
	// ObjectID is the record's stable identifier.
	ObjectID string `json:"objectID"`
}

// ObjectRecord contains a record's identifier and optional display name.
type ObjectRecord struct {
	// ObjectID is the record's stable identifier.
	ObjectID string `json:"objectID"`
	// Name is the record's optional display name.
	Name string `json:"name,omitempty"`
}

// ListIndicesInput selects one bounded index page; zero values select the first default-size page.
type ListIndicesInput struct {
	// Page is the zero-based page number.
	Page int `json:"page"`
	// HitsPerPage is the positive page size, at most 1000; zero uses Algolia's default.
	HitsPerPage int `json:"hitsPerPage,omitempty"`
}

// IndexSummary identifies one index and its record count.
type IndexSummary struct {
	// Name is the index name.
	Name string `json:"name"`
	// Entries is the number of indexed records.
	Entries int `json:"entries"`
}

// IndexPage contains one page of Algolia index summaries.
type IndexPage struct {
	// Items are the indices in this page.
	Items []IndexSummary `json:"items"`
	// NbPages is the provider's total page count.
	NbPages int `json:"nbPages"`
}

// PartialUpdateObjectInput sets selected string attributes on an existing record.
type PartialUpdateObjectInput struct {
	// IndexName names the existing index.
	IndexName string `json:"indexName"`
	// ObjectID is the record's stable identifier.
	ObjectID string `json:"objectID"`
	// Attributes contains absolute string values to set, never increment actions.
	Attributes map[string]string `json:"attributes"`
}

// SearchOperation searches one index.
type SearchOperation struct{ client *Client }

// SaveObjectOperation replaces a record by object ID.
type SaveObjectOperation struct{ client *Client }

// GetObjectOperation retrieves one record by object ID.
type GetObjectOperation struct{ client *Client }

// ListIndicesOperation lists a bounded page of indices.
type ListIndicesOperation struct{ client *Client }

// PartialUpdateObjectOperation sets selected absolute string attributes.
type PartialUpdateObjectOperation struct{ client *Client }

// DeleteObjectOperation deletes one record by object ID.
type DeleteObjectOperation struct{ client *Client }

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

// GetObject binds record retrieval to this client.
func (client *Client) GetObject() GetObjectOperation { return GetObjectOperation{client} }

// ListIndices binds index listing to this client.
func (client *Client) ListIndices() ListIndicesOperation { return ListIndicesOperation{client} }

// PartialUpdateObject binds an absolute attribute update to this client.
func (client *Client) PartialUpdateObject() PartialUpdateObjectOperation {
	return PartialUpdateObjectOperation{client}
}

// DeleteObject binds record deletion to this client.
func (client *Client) DeleteObject() DeleteObjectOperation { return DeleteObjectOperation{client} }

// Definition returns the generated search contract.
func (SearchOperation) Definition() sdkgo.QueryDefinition { return SearchDefinition }

// Definition returns the generated record replacement contract.
func (SaveObjectOperation) Definition() sdkgo.MutationDefinition { return SaveObjectDefinition }

// Definition returns the generated retrieval contract.
func (GetObjectOperation) Definition() sdkgo.QueryDefinition { return GetObjectDefinition }

// Definition returns the generated index listing contract.
func (ListIndicesOperation) Definition() sdkgo.QueryDefinition { return ListIndicesDefinition }

// Definition returns the generated attribute update contract.
func (PartialUpdateObjectOperation) Definition() sdkgo.MutationDefinition {
	return PartialUpdateObjectDefinition
}

// Definition returns the generated deletion contract.
func (DeleteObjectOperation) Definition() sdkgo.MutationDefinition { return DeleteObjectDefinition }

// IdempotencyKey uses Dex's call identity; a stable object ID and absolute replacement make retries safe.
func (SaveObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ SaveObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the stable call identity for an absolute attribute update.
func (PartialUpdateObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ PartialUpdateObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the stable call identity for a deletion.
func (DeleteObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ ObjectInput) sdkgo.IdempotencyKey {
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

// Invoke retrieves the record identity and display name without exposing arbitrary record fields.
func (operation GetObjectOperation) Invoke(call sdkgo.Call, input ObjectInput) sdkgo.QueryAttempt[ObjectRecord] {
	const name = "getObject"
	if !validPathPart(input.IndexName) || !validPathPart(input.ObjectID) {
		return sdkgo.NewQueryBranch(GetObjectBranchDefect, ObjectRecord{}, algoliaFailure(name, sdkgo.FailureValidation, "indexName and objectID are required"), sdkgo.Receipt{})
	}
	path := "/indexes/" + url.PathEscape(input.IndexName) + "/" + url.PathEscape(input.ObjectID)
	body, status, failure, retryAfter := operation.client.send(call, http.MethodGet, path, nil, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewQueryBranch(GetObjectBranchDefect, ObjectRecord{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewQueryRetry[ObjectRecord](*failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(GetObjectBranchInvalidResponse, ObjectRecord{}, failure, sdkgo.Receipt{})
	}
	if status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetObjectBranchMissing, ObjectRecord{}, nil, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(GetObjectBranchProviderRejected, ObjectRecord{}, algoliaFailure(name, rejectionKind(status), "Algolia rejected record retrieval with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var record ObjectRecord
	if json.Unmarshal(body, &record) != nil || record.ObjectID != input.ObjectID {
		return sdkgo.NewQueryBranch(GetObjectBranchInvalidResponse, ObjectRecord{}, algoliaFailure(name, sdkgo.FailureProtocol, "Algolia returned an invalid record"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetObjectBranchFound, record, nil, sdkgo.Receipt{})
}

// Invoke lists one bounded page of index names and counts.
func (operation ListIndicesOperation) Invoke(call sdkgo.Call, input ListIndicesInput) sdkgo.QueryAttempt[IndexPage] {
	const name = "listIndices"
	if input.Page < 0 || input.HitsPerPage < 0 || input.HitsPerPage > 1000 {
		return sdkgo.NewQueryBranch(ListIndicesBranchDefect, IndexPage{}, algoliaFailure(name, sdkgo.FailureValidation, "page or hitsPerPage is invalid"), sdkgo.Receipt{})
	}
	parameters := url.Values{"page": {strconv.Itoa(input.Page)}}
	if input.HitsPerPage > 0 {
		parameters.Set("hitsPerPage", strconv.Itoa(input.HitsPerPage))
	}
	body, status, failure, retryAfter := operation.client.send(call, http.MethodGet, "/indexes?"+parameters.Encode(), nil, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewQueryBranch(ListIndicesBranchDefect, IndexPage{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewQueryRetry[IndexPage](*failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(ListIndicesBranchInvalidResponse, IndexPage{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListIndicesBranchProviderRejected, IndexPage{}, algoliaFailure(name, rejectionKind(status), "Algolia rejected index listing with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var page IndexPage
	if json.Unmarshal(body, &page) != nil || page.Items == nil || page.NbPages < 0 {
		return sdkgo.NewQueryBranch(ListIndicesBranchInvalidResponse, IndexPage{}, algoliaFailure(name, sdkgo.FailureProtocol, "Algolia returned an invalid index page"), sdkgo.Receipt{})
	}
	for _, item := range page.Items {
		if item.Name == "" || item.Entries < 0 {
			return sdkgo.NewQueryBranch(ListIndicesBranchInvalidResponse, IndexPage{}, algoliaFailure(name, sdkgo.FailureProtocol, "Algolia returned an invalid index"), sdkgo.Receipt{})
		}
	}
	return sdkgo.NewQueryBranch(ListIndicesBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke sets absolute string attributes of an existing record.
func (operation PartialUpdateObjectOperation) Invoke(call sdkgo.Call, input PartialUpdateObjectInput) sdkgo.MutationAttempt[SavedObject] {
	const name = "partialUpdateObject"
	if !validPathPart(input.IndexName) || !validPathPart(input.ObjectID) || len(input.Attributes) == 0 {
		return sdkgo.NewMutationBranch(PartialUpdateObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "indexName, objectID, and attributes are required"), sdkgo.Receipt{})
	}
	if _, reserved := input.Attributes["objectID"]; reserved {
		return sdkgo.NewMutationBranch(PartialUpdateObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "objectID belongs in its dedicated field"), sdkgo.Receipt{})
	}
	for key := range input.Attributes {
		if strings.TrimSpace(key) == "" {
			return sdkgo.NewMutationBranch(PartialUpdateObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "attribute names must be nonblank"), sdkgo.Receipt{})
		}
	}
	payload, err := json.Marshal(input.Attributes)
	if err != nil || len(payload) > 1<<20 {
		return sdkgo.NewMutationBranch(PartialUpdateObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "attribute payload is invalid or too large"), sdkgo.Receipt{})
	}
	path := "/indexes/" + url.PathEscape(input.IndexName) + "/" + url.PathEscape(input.ObjectID) + "/partial?createIfNotExists=false"
	body, status, failure, retryAfter := operation.client.send(call, http.MethodPost, path, payload, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewMutationBranch(PartialUpdateObjectBranchDefect, SavedObject{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewMutationRetry[SavedObject](*failure, retryAfter)
		}
		return sdkgo.NewMutationBranch(PartialUpdateObjectBranchInvalidResponse, SavedObject{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewMutationBranch(PartialUpdateObjectBranchProviderRejected, SavedObject{}, algoliaFailure(name, rejectionKind(status), "Algolia rejected attribute update with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var result SavedObject
	if json.Unmarshal(body, &result) != nil || result.ObjectID != input.ObjectID || result.TaskID <= 0 {
		return sdkgo.NewMutationBranch(PartialUpdateObjectBranchInvalidResponse, SavedObject{}, algoliaFailure(name, sdkgo.FailureProtocol, "Algolia returned an invalid indexing task"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(PartialUpdateObjectBranchUpdated, result, nil, sdkgo.Receipt{})
}

// Invoke queues deletion of a record by stable object ID.
func (operation DeleteObjectOperation) Invoke(call sdkgo.Call, input ObjectInput) sdkgo.MutationAttempt[SavedObject] {
	const name = "deleteObject"
	if !validPathPart(input.IndexName) || !validPathPart(input.ObjectID) {
		return sdkgo.NewMutationBranch(DeleteObjectBranchDefect, SavedObject{}, algoliaFailure(name, sdkgo.FailureValidation, "indexName and objectID are required"), sdkgo.Receipt{})
	}
	path := "/indexes/" + url.PathEscape(input.IndexName) + "/" + url.PathEscape(input.ObjectID)
	body, status, failure, retryAfter := operation.client.send(call, http.MethodDelete, path, nil, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewMutationBranch(DeleteObjectBranchDefect, SavedObject{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewMutationRetry[SavedObject](*failure, retryAfter)
		}
		return sdkgo.NewMutationBranch(DeleteObjectBranchInvalidResponse, SavedObject{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewMutationBranch(DeleteObjectBranchProviderRejected, SavedObject{}, algoliaFailure(name, rejectionKind(status), "Algolia rejected record deletion with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var result SavedObject
	if json.Unmarshal(body, &result) != nil || result.ObjectID != input.ObjectID || result.TaskID <= 0 {
		return sdkgo.NewMutationBranch(DeleteObjectBranchInvalidResponse, SavedObject{}, algoliaFailure(name, sdkgo.FailureProtocol, "Algolia returned an invalid deletion task"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteObjectBranchDeleted, result, nil, sdkgo.Receipt{})
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
