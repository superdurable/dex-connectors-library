// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,220}[a-z0-9]$`)
var generationPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

// ListObjectsInput selects one page from a named bucket.
type ListObjectsInput struct {
	// Bucket is the non-secret bucket name shown in Cloud Storage Buckets.
	Bucket string `json:"bucket"`
	// Prefix limits names to this optional object name prefix.
	Prefix string `json:"prefix"`
	// PageSize is 1 through 1000; zero uses 100.
	PageSize int `json:"pageSize"`
	// PageToken is Google's opaque cursor; blank selects the first page.
	PageToken string `json:"pageToken"`
}

// Object describes one object identity and immutable generation.
type Object struct {
	// Name is the exact object name returned by Google.
	Name string `json:"name"`
	// Generation selects a specific immutable object version.
	Generation string `json:"generation"`
}

// ObjectsPage contains one bounded page and its next cursor.
type ObjectsPage struct {
	// Objects are projected object identities.
	Objects []Object `json:"objects"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// DownloadObjectInput selects one bounded object body.
type DownloadObjectInput struct {
	// Bucket is the non-secret bucket name shown in Cloud Storage Buckets.
	Bucket string `json:"bucket"`
	// Name is the exact object name returned by listObjects.
	Name string `json:"name"`
	// Generation optionally pins an immutable generation returned by listObjects.
	Generation string `json:"generation"`
}

// DownloadedObject carries bounded bytes encoded for a JSON Flow result.
type DownloadedObject struct {
	// ContentBase64 is the downloaded object data; decode before use.
	ContentBase64 string `json:"contentBase64"`
	// ContentType is the HTTP media type supplied by Google.
	ContentType string `json:"contentType"`
}

// ListObjectsOperation reads one page of object identities.
type ListObjectsOperation struct{ client *Client }

// DownloadObjectOperation reads one object body.
type DownloadObjectOperation struct{ client *Client }

// ListObjects binds the object page Query.
func (client *Client) ListObjects() ListObjectsOperation { return ListObjectsOperation{client} }

// DownloadObject binds the object download Query.
func (client *Client) DownloadObject() DownloadObjectOperation {
	return DownloadObjectOperation{client}
}

// Definition returns the generated object page Query contract.
func (ListObjectsOperation) Definition() sdkgo.QueryDefinition { return ListObjectsDefinition }

// Definition returns the generated object download Query contract.
func (DownloadObjectOperation) Definition() sdkgo.QueryDefinition { return DownloadObjectDefinition }

// Invoke lists object identities without fetching their content.
func (operation ListObjectsOperation) Invoke(call sdkgo.Call, input ListObjectsInput) sdkgo.QueryAttempt[ObjectsPage] {
	if !bucketNamePattern.MatchString(input.Bucket) || input.PageSize < 0 || input.PageSize > 1000 || !validObjectNamePrefix(input.Prefix) || !validPageToken(input.PageToken) {
		return queryBranch("listObjects", "defect", ObjectsPage{}, sdkgo.FailureValidation, "object page input is invalid")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	query := url.Values{"maxResults": {strconv.Itoa(pageSize)}}
	if input.Prefix != "" {
		query.Set("prefix", input.Prefix)
	}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.request(call, "/storage/v1/b/"+url.PathEscape(input.Bucket)+"/o?"+query.Encode(), false)
	if decision, failed := classifyRead[ObjectsPage](response, "listObjects"); failed {
		return decision
	}
	var wire struct {
		Items         []Object `json:"items"`
		NextPageToken string   `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || len(wire.Items) > pageSize || !validPageToken(wire.NextPageToken) {
		return queryBranch("listObjects", "invalidResponse", ObjectsPage{}, sdkgo.FailureProtocol, "Google returned an invalid object page")
	}
	for _, object := range wire.Items {
		if !validObjectName(object.Name) || !generationPattern.MatchString(object.Generation) {
			return queryBranch("listObjects", "invalidResponse", ObjectsPage{}, sdkgo.FailureProtocol, "Google returned an invalid object identity")
		}
	}
	return sdkgo.NewQueryBranch(ListObjectsBranchListed, ObjectsPage{Objects: wire.Items, NextPageToken: wire.NextPageToken}, nil, sdkgo.Receipt{})
}

// Invoke downloads one bounded object version.
func (operation DownloadObjectOperation) Invoke(call sdkgo.Call, input DownloadObjectInput) sdkgo.QueryAttempt[DownloadedObject] {
	if !bucketNamePattern.MatchString(input.Bucket) || !validObjectName(input.Name) || (input.Generation != "" && !generationPattern.MatchString(input.Generation)) {
		return queryBranch("downloadObject", "defect", DownloadedObject{}, sdkgo.FailureValidation, "object identity is invalid")
	}
	query := url.Values{"alt": {"media"}}
	if input.Generation != "" {
		query.Set("generation", input.Generation)
	}
	response := operation.client.request(call, "/storage/v1/b/"+url.PathEscape(input.Bucket)+"/o/"+url.PathEscape(input.Name)+"?"+query.Encode(), true)
	if response.status == 404 && response.err == nil {
		return queryBranch("downloadObject", "notFound", DownloadedObject{}, sdkgo.FailureProviderRejection, "object was not found")
	}
	if decision, failed := classifyRead[DownloadedObject](response, "downloadObject"); failed {
		return decision
	}
	return sdkgo.NewQueryBranch(DownloadObjectBranchDownloaded, DownloadedObject{ContentBase64: base64.StdEncoding.EncodeToString(response.body), ContentType: response.contentType}, nil, sdkgo.Receipt{})
}

func classifyRead[T any](response providerResponse, operation string) (sdkgo.QueryAttempt[T], bool) {
	var zero T
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, "defect", zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRedirect) {
		return queryBranch(operation, "invalidResponse", zero, sdkgo.FailureProtocol, "Google returned an unsafe download redirect"), true
	}
	if errors.Is(response.err, errRequest) {
		return queryRetry[T](operation, sdkgo.FailureTransport, "Google Cloud Storage request failed", 0), true
	}
	if response.status == 429 {
		return queryRetry[T](operation, sdkgo.FailureRateLimit, "Google rate limited the request", response.retryAfter), true
	}
	if response.status == 408 || response.status >= 500 {
		return queryRetry[T](operation, sdkgo.FailureAvailability, "Google Cloud Storage is temporarily unavailable", response.retryAfter), true
	}
	if response.status != 200 {
		return queryBranch(operation, "providerRejected", zero, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, "invalidResponse", zero, sdkgo.FailureResponseTooLarge, "Google response exceeded the configured limit"), true
	}
	if response.err != nil {
		return queryRetry[T](operation, sdkgo.FailureTransport, "Google response was interrupted", 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func queryBranch[T any](operation, branch string, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(sdkgo.BranchID(branch), value, &failure, sdkgo.Receipt{})
}
func queryRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-cloud-storage", Operation: operation, Message: message}
	return sdkgo.NewQueryRetry[T](failure, delay)
}
func validObjectName(value string) bool {
	return value != "" && len(value) <= 1024 && !strings.ContainsAny(value, "\r\n")
}
func validObjectNamePrefix(value string) bool {
	return len(value) <= 1024 && !strings.ContainsAny(value, "\r\n")
}
func validPageToken(value string) bool {
	return len(value) <= 4096 && !strings.ContainsAny(value, "\r\n")
}
