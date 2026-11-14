// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiguang

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListApplicationTagsInput has no parameters; the connection identifies the app.
type ListApplicationTagsInput struct{}

// ApplicationTags contains names returned by the JPush tag list API.
type ApplicationTags struct {
	// Tags are tag names for the configured JPush application.
	Tags []string `json:"tags"`
}

// ListApplicationTagsOperation reads application tags without device identifiers.
type ListApplicationTagsOperation struct{ client *Client }

// ListApplicationTags binds the Query to this client.
func (client *Client) ListApplicationTags() ListApplicationTagsOperation {
	return ListApplicationTagsOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (ListApplicationTagsOperation) Definition() sdkgo.QueryDefinition {
	return ListApplicationTagsDefinition
}

// Invoke queries JPush and classifies the provider outcome.
func (operation ListApplicationTagsOperation) Invoke(call sdkgo.Call, _ ListApplicationTagsInput) sdkgo.QueryAttempt[ApplicationTags] {
	response := operation.client.listTags(call)
	if errors.Is(response.err, errCredential) {
		return tagsBranch(ListApplicationTagsBranchDefect, sdkgo.FailureAuthentication, "JPush credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ApplicationTags](tagsFailure(sdkgo.FailureTransport, "JPush request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ApplicationTags](tagsFailure(sdkgo.FailureRateLimit, "JPush rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ApplicationTags](tagsFailure(sdkgo.FailureAvailability, "JPush is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return tagsBranch(ListApplicationTagsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "JPush tag response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ApplicationTags](tagsFailure(sdkgo.FailureTransport, "JPush response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return tagsBranch(ListApplicationTagsBranchProviderRejected, sdkgo.FailureProviderRejection, "JPush rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return tagsBranch(ListApplicationTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JPush returned invalid UTF-8")
	}
	var wire struct {
		Tags json.RawMessage `json:"tags"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return tagsBranch(ListApplicationTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JPush returned invalid tags JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return tagsBranch(ListApplicationTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JPush returned trailing JSON")
	}
	if len(wire.Tags) == 0 {
		return tagsBranch(ListApplicationTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JPush omitted tags")
	}
	tags := []string{}
	if !bytes.Equal(wire.Tags, []byte("null")) {
		if json.Unmarshal(wire.Tags, &tags) != nil {
			return tagsBranch(ListApplicationTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JPush returned invalid tags")
		}
	}
	if len(tags) > 1000 {
		return tagsBranch(ListApplicationTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JPush returned too many tags")
	}
	for _, tag := range tags {
		if tag == "" || len(tag) > 40 {
			return tagsBranch(ListApplicationTagsBranchInvalidResponse, sdkgo.FailureProtocol, "JPush returned an invalid tag name")
		}
	}
	return sdkgo.NewQueryBranch(ListApplicationTagsBranchListed, ApplicationTags{Tags: tags}, nil, sdkgo.Receipt{})
}
func tagsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "jiguang", Operation: "listApplicationTags", Message: message}
}
func tagsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ApplicationTags] {
	failure := tagsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ApplicationTags{}, &failure, sdkgo.Receipt{})
}
