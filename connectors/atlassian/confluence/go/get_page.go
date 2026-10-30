// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getPageOperationID    = "getPage"
	getPageFailureSubject = "page"
)

// GetPageInput identifies one page and how to return its body.
type GetPageInput struct {
	// PageID is the numeric page ID, such as 123456.
	PageID string `json:"pageId"`
	// BodyRepresentation selects the Confluence body to convert; blank reads storage format.
	BodyRepresentation BodyRepresentation `json:"bodyRepresentation,omitempty"`
	// BodyFormat selects the returned text; blank returns Markdown.
	BodyFormat TextFormat `json:"bodyFormat,omitempty"`
	// MaxBodyCharacters cuts Body after this many characters, 1 to 131072; zero keeps 32768.
	MaxBodyCharacters int `json:"maxBodyCharacters,omitempty"`
}

// GetPageOperation implements the getPage Query.
type GetPageOperation struct{ client *Client }

// getPageRequest is validated getPage input with defaults applied.
type getPageRequest struct {
	pageID         string
	representation BodyRepresentation
	format         TextFormat
	maxCharacters  int
}

// Definition returns the immutable connector operation definition.
func (GetPageOperation) Definition() sdkgo.QueryDefinition { return GetPageDefinition }

// Invoke reads one page. Transport failures, 408, 429, and 5xx responses are retried.
func (operation GetPageOperation) Invoke(call sdkgo.Call, input GetPageInput) sdkgo.QueryAttempt[Page] {
	client := operation.client
	request, err := buildGetPageRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetPageBranchDefect, Page{}, failurePointer(getPageOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, getPageOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewQueryRetry[Page](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewQueryBranch(GetPageBranchDefect, Page{}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, confluenceRequest{
		method: http.MethodGet, api: contentAPI, path: pagePath(request.pageID), query: url.Values{"body-format": {string(request.representation)}},
	})
	classification := client.classifyRead(getPageOperationID, getPageFailureSubject, result)
	receipt := client.receipt(session, result.response, request.pageID)
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[Page](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(GetPageBranchNotFound, Page{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(GetPageBranchDefect, Page{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(GetPageBranchInvalidResponse, Page{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetPageBranchProviderRejected, Page{}, &classification.failure, receipt)
	}
	var resource pageResource
	if err := json.Unmarshal(result.response.body, &resource); err != nil {
		return sdkgo.NewQueryBranch(GetPageBranchInvalidResponse, Page{}, failurePointer(getPageOperationID, sdkgo.FailureProtocol, "Confluence returned an invalid page"), receipt)
	}
	page, err := decodePage(resource, "", request.representation, request.format, request.maxCharacters)
	if err != nil {
		return sdkgo.NewQueryBranch(GetPageBranchInvalidResponse, Page{}, failurePointer(getPageOperationID, sdkgo.FailureProtocol, "Confluence returned an unreadable page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetPageBranchFound, page, nil, receipt)
}

func buildGetPageRequest(input GetPageInput) (getPageRequest, error) {
	pageID, err := validateContentID(input.PageID, "pageId")
	if err != nil {
		return getPageRequest{}, err
	}
	request := getPageRequest{pageID: pageID, representation: input.BodyRepresentation, format: input.BodyFormat, maxCharacters: input.MaxBodyCharacters}
	switch request.representation {
	case "":
		request.representation = BodyRepresentationStorage
	case BodyRepresentationStorage, BodyRepresentationAtlasDocFormat:
	default:
		return getPageRequest{}, errors.New("bodyRepresentation must be storage or atlas_doc_format")
	}
	switch request.format {
	case "":
		request.format = TextFormatMarkdown
	case TextFormatMarkdown, TextFormatPlainText:
	default:
		return getPageRequest{}, errors.New("bodyFormat must be markdown or plainText")
	}
	switch {
	case request.maxCharacters == 0:
		request.maxCharacters = defaultBodyCharacters
	case request.maxCharacters < 1 || request.maxCharacters > maximumBodyCharacters:
		return getPageRequest{}, errors.New("maxBodyCharacters must be between 1 and 131072")
	}
	return request, nil
}
