// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package figma implements read-only Figma API Queries.
package figma

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Figma.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Figma credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Figma base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var (
	safeKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

// GetFileInput identifies a Figma file or branch by key.
type GetFileInput struct {
	// FileKey is the file or branch key from a Figma URL.
	FileKey string `json:"fileKey"`
}

// File contains Figma file metadata and its bounded document tree.
type File struct {
	// Name is the resource display name.
	Name string `json:"name"`
	// LastModified is the file's provider-reported timestamp.
	LastModified string `json:"lastModified"`
	// Document is the bounded Figma document JSON tree.
	Document json.RawMessage `json:"document"`
}

// GetFileCommentsInput identifies a Figma file or branch by key.
type GetFileCommentsInput struct {
	// FileKey is the file or branch key from a Figma URL.
	FileKey string `json:"fileKey"`
}

// FileComment is a comment identity and text.
type FileComment struct {
	// ID is the provider-assigned resource identity.
	ID string `json:"id"`
	// Message is the comment text.
	Message string `json:"message"`
}

// FileComments contains comments on one file.
type FileComments struct {
	// Comments contains comments returned for the file.
	Comments []FileComment `json:"comments"`
}

// GetFile returns the bound getFile Query.
func (client *Client) GetFile() GetFileOperation { return GetFileOperation{client} }

// GetFileOperation performs one Figma read.
type GetFileOperation struct{ client *Client }

// Definition returns the manifest-generated operation contract.
func (GetFileOperation) Definition() sdkgo.QueryDefinition { return GetFileDefinition }

// Invoke validates input and classifies the HTTP result.
func (operation GetFileOperation) Invoke(call sdkgo.Call, input GetFileInput) sdkgo.QueryAttempt[File] {
	if !safeKey.MatchString(input.FileKey) {
		return failedBranch[File]("getFile", GetFileBranchDefect, sdkgo.FailureValidation, "fileKey is invalid")
	}
	path := "/files/" + input.FileKey
	return invokeGet(operation.client, call, "getFile", path, GetFileBranchFound, GetFileBranchNotFound, decodeGetFile)
}
func decodeGetFile(body []byte) (File, error) {
	var file File
	if err := json.Unmarshal(body, &file); err != nil {
		return File{}, err
	}
	if file.Name == "" || len(file.Document) == 0 || string(file.Document) == "null" {
		return File{}, fmt.Errorf("incomplete Figma file")
	}
	return file, nil
}

// GetFileComments returns the bound getFileComments Query.
func (client *Client) GetFileComments() GetFileCommentsOperation {
	return GetFileCommentsOperation{client}
}

// GetFileCommentsOperation performs one Figma read.
type GetFileCommentsOperation struct{ client *Client }

// Definition returns the manifest-generated operation contract.
func (GetFileCommentsOperation) Definition() sdkgo.QueryDefinition { return GetFileCommentsDefinition }

// Invoke validates input and classifies the HTTP result.
func (operation GetFileCommentsOperation) Invoke(call sdkgo.Call, input GetFileCommentsInput) sdkgo.QueryAttempt[FileComments] {
	if !safeKey.MatchString(input.FileKey) {
		return failedBranch[FileComments]("getFileComments", GetFileCommentsBranchDefect, sdkgo.FailureValidation, "fileKey is invalid")
	}
	path := "/files/" + input.FileKey + "/comments"
	return invokeGet(operation.client, call, "getFileComments", path, GetFileCommentsBranchListed, GetFileCommentsBranchNotFound, decodeGetFileComments)
}
func decodeGetFileComments(body []byte) (FileComments, error) {
	var result FileComments
	if err := json.Unmarshal(body, &result); err != nil {
		return FileComments{}, err
	}
	if result.Comments == nil {
		return FileComments{}, fmt.Errorf("missing comments")
	}
	return result, nil
}

func invokeGet[T any](client *Client, call sdkgo.Call, operationID, path string, successBranch, notFoundBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureLocalDefect, "request cannot be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Figma-Token", credentials.APIKey.Reveal())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider request failed", 0)
	}
	// Closing a read response cannot change its outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		switch {
		case response.StatusCode == http.StatusTooManyRequests:
			return retryBranch[T](operationID, sdkgo.FailureRateLimit, "provider rate limit", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError:
			return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == http.StatusNotFound && notFoundBranch != "providerRejected":
			return failedBranch[T](operationID, notFoundBranch, sdkgo.FailureNotFound, "provider resource was not found")
		case response.StatusCode == http.StatusUnauthorized:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthentication, "provider rejected the API key")
		case response.StatusCode == http.StatusForbidden:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthorization, "provider denied the request")
		default:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureProviderRejection, "provider rejected HTTP "+strconv.Itoa(response.StatusCode))
		}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureResponseTooLarge, "provider response exceeded configured limit")
	}
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider response was interrupted", 0)
	}
	value, err := decode(body)
	if err != nil {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureProtocol, "provider returned unusable JSON")
	}
	return sdkgo.NewQueryBranch(successBranch, value, nil, sdkgo.Receipt{})
}
func failedBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "figma", Operation: operationID, Message: message}
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func retryBranch[T any](operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "figma", Operation: operationID, Message: message}
	return sdkgo.NewQueryRetry[T](failure, after)
}
