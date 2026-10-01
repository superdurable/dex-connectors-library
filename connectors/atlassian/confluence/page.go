// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	maximumTitleCharacters = 255
	// defaultBodyCharacters bounds converted page text so a Result stays small enough for Flow state.
	defaultBodyCharacters = 32768
	maximumBodyCharacters = 131072
	titleLookupLimit      = 5
)

// BodyRepresentation selects the Confluence body representation getPage reads and converts.
type BodyRepresentation string

const (
	// BodyRepresentationStorage is Confluence's XHTML storage format. It is the default.
	BodyRepresentationStorage BodyRepresentation = "storage"
	// BodyRepresentationAtlasDocFormat is Atlassian Document Format, the JSON the current editor uses.
	BodyRepresentationAtlasDocFormat BodyRepresentation = "atlas_doc_format"
)

// PageVersion is one published version of a page.
type PageVersion struct {
	// Number is the version number; an update must name Number plus one.
	Number int `json:"number"`
	// CreatedAt is when the version was saved.
	CreatedAt time.Time `json:"createdAt"`
	// AuthorAccountID is the Atlassian account that saved the version.
	AuthorAccountID string `json:"authorAccountId,omitempty"`
	// IsMinorEdit reports a version saved without notifications.
	IsMinorEdit bool `json:"isMinorEdit,omitempty"`
}

// Page is the connector's view of one Confluence page.
type Page struct {
	// ID is the numeric page ID, stable across title changes and moves.
	ID string `json:"id"`
	// Title is the page title, unique within its space.
	Title string `json:"title"`
	// Status is Confluence's page status, such as current or archived.
	Status string `json:"status"`
	// SpaceID is the numeric ID of the space that holds the page.
	SpaceID string `json:"spaceId"`
	// ParentPageID is the parent page's ID, or empty for a page at the space root.
	ParentPageID string `json:"parentPageId,omitempty"`
	// AuthorAccountID is the account that created the page.
	AuthorAccountID string `json:"authorAccountId,omitempty"`
	// CreatedAt is when the page was created.
	CreatedAt time.Time `json:"createdAt"`
	// Version is the current published version.
	Version PageVersion `json:"version"`
	// Body is the page body converted to BodyFormat, cut at the requested character limit.
	Body string `json:"body"`
	// BodyFormat is the format of Body.
	BodyFormat TextFormat `json:"bodyFormat"`
	// IsBodyTruncated reports that Body stopped at the character limit.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// WebURL is the page's address in the Confluence web UI, or empty when Confluence omitted it.
	WebURL string `json:"webUrl,omitempty"`
}

type pageResource struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Title     string            `json:"title"`
	SpaceID   string            `json:"spaceId"`
	ParentID  string            `json:"parentId"`
	AuthorID  string            `json:"authorId"`
	CreatedAt string            `json:"createdAt"`
	Version   *versionResource  `json:"version"`
	Body      *pageBodyResource `json:"body"`
	Links     pageLinksResource `json:"_links"`
}

type versionResource struct {
	Number    int    `json:"number"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
	AuthorID  string `json:"authorId"`
	MinorEdit bool   `json:"minorEdit"`
}

type pageBodyResource struct {
	Storage        *bodyValueResource `json:"storage"`
	AtlasDocFormat *bodyValueResource `json:"atlas_doc_format"`
}

type bodyValueResource struct {
	Representation string `json:"representation"`
	Value          string `json:"value"`
}

type pageLinksResource struct {
	WebUI string `json:"webui"`
	Base  string `json:"base"`
}

type pageListResource struct {
	Results []pageResource    `json:"results"`
	Links   pageLinksResource `json:"_links"`
}

type spaceResource struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

type spaceListResource struct {
	Results []spaceResource `json:"results"`
}

// pageLookup is the outcome of one read used to reconcile a write.
type pageLookup struct {
	classification readClassification
	page           *pageResource
	siteBase       string
	response       confluenceResponse
}

// readPage reads one page with its storage body, for reconciliation after a write.
func (client *Client) readPage(session *operationSession, operationID string, pageID string) pageLookup {
	result := client.exchange(session, confluenceRequest{
		method: http.MethodGet, api: contentAPI, path: pagePath(pageID), query: url.Values{"body-format": {string(BodyRepresentationStorage)}},
	})
	lookup := pageLookup{classification: client.classifyRead(operationID, "page", result), response: result.response}
	if lookup.classification.outcome != readSucceeded {
		return lookup
	}
	var resource pageResource
	if err := json.Unmarshal(result.response.body, &resource); err != nil || validatePageResource(resource) != nil {
		lookup.classification = readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Confluence returned an invalid page")}
		return lookup
	}
	lookup.page, lookup.siteBase = &resource, resource.Links.Base
	return lookup
}

// readPageVersionMessage reads the message saved with one version of a page.
func (client *Client) readPageVersionMessage(session *operationSession, operationID string, pageID string, versionNumber int) (string, readClassification) {
	result := client.exchange(session, confluenceRequest{
		method: http.MethodGet, api: contentAPI, path: pagePath(pageID) + "/versions/" + strconv.Itoa(versionNumber),
	})
	classification := client.classifyRead(operationID, "page version", result)
	if classification.outcome != readSucceeded {
		return "", classification
	}
	var version versionResource
	if err := json.Unmarshal(result.response.body, &version); err != nil || version.Number != versionNumber {
		return "", readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Confluence returned an invalid page version")}
	}
	return version.Message, classification
}

// findPageByTitle reads the current or archived page with exactly this title in the space, if any.
func (client *Client) findPageByTitle(session *operationSession, operationID string, spaceID string, title string) pageLookup {
	result := client.exchange(session, confluenceRequest{
		method: http.MethodGet, api: contentAPI, path: "/pages",
		query: url.Values{"space-id": {spaceID}, "title": {title}, "body-format": {string(BodyRepresentationStorage)}, "limit": {strconv.Itoa(titleLookupLimit)}},
	})
	lookup := pageLookup{classification: client.classifyRead(operationID, "page lookup", result), response: result.response}
	if lookup.classification.outcome != readSucceeded {
		return lookup
	}
	var list pageListResource
	if err := json.Unmarshal(result.response.body, &list); err != nil || len(list.Results) > titleLookupLimit {
		lookup.classification = readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Confluence returned an invalid page list")}
		return lookup
	}
	lookup.siteBase = list.Links.Base
	for index := range list.Results {
		resource := list.Results[index]
		if validatePageResource(resource) != nil {
			lookup.classification = readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Confluence returned an invalid page in the list")}
			return lookup
		}
		if resource.Title == title && resource.SpaceID == spaceID {
			lookup.page = &resource
			return lookup
		}
	}
	return lookup
}

// resolveSpaceID returns the numeric space ID for a space key; a missing space selects readNotFound.
func (client *Client) resolveSpaceID(session *operationSession, operationID string, spaceKey string) (string, readClassification) {
	result := client.exchange(session, confluenceRequest{
		method: http.MethodGet, api: contentAPI, path: "/spaces", query: url.Values{"keys": {spaceKey}, "limit": {"1"}},
	})
	classification := client.classifyRead(operationID, "space", result)
	if classification.outcome != readSucceeded {
		return "", classification
	}
	var list spaceListResource
	if err := json.Unmarshal(result.response.body, &list); err != nil {
		return "", readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Confluence returned an invalid space list")}
	}
	for _, space := range list.Results {
		if strings.EqualFold(space.Key, spaceKey) && contentIDPattern.MatchString(space.ID) {
			return space.ID, classification
		}
	}
	return "", readClassification{outcome: readNotFound, failure: newFailure(operationID, sdkgo.FailureNotFound, "Confluence space "+spaceKey+" was not found or is not visible to the connection")}
}

// decodePage converts a page resource to the connector's view, converting the requested body.
func decodePage(resource pageResource, siteBase string, representation BodyRepresentation, format TextFormat, maxCharacters int) (Page, error) {
	if err := validatePageResource(resource); err != nil {
		return Page{}, err
	}
	page := Page{
		ID: resource.ID, Title: resource.Title, Status: resource.Status, SpaceID: resource.SpaceID,
		AuthorAccountID: safeAccountID(resource.AuthorID), BodyFormat: format,
		Version: PageVersion{
			Number: resource.Version.Number, AuthorAccountID: safeAccountID(resource.Version.AuthorID), IsMinorEdit: resource.Version.MinorEdit,
		},
		WebURL: buildWebURL(firstNonEmpty(resource.Links.Base, siteBase), resource.Links.WebUI),
	}
	if contentIDPattern.MatchString(resource.ParentID) {
		page.ParentPageID = resource.ParentID
	}
	// Timestamps are informational; an unparseable one leaves the zero time instead of rejecting the page.
	page.CreatedAt, _ = time.Parse(time.RFC3339Nano, resource.CreatedAt)
	page.Version.CreatedAt, _ = time.Parse(time.RFC3339Nano, resource.Version.CreatedAt)
	blocks, err := parsePageBody(resource.Body, representation)
	if err != nil {
		return Page{}, err
	}
	page.Body, page.IsBodyTruncated = renderDocumentText(blocks, format, maxCharacters)
	return page, nil
}

func parsePageBody(body *pageBodyResource, representation BodyRepresentation) ([]documentBlock, error) {
	switch representation {
	case BodyRepresentationAtlasDocFormat:
		if body == nil || body.AtlasDocFormat == nil {
			return nil, errors.New("Confluence returned no atlas_doc_format body")
		}
		return parseADFDocument(body.AtlasDocFormat.Value)
	default:
		if body == nil || body.Storage == nil {
			return nil, errors.New("Confluence returned no storage body")
		}
		return parseStorageDocument(body.Storage.Value)
	}
}

func validatePageResource(resource pageResource) error {
	switch {
	case !contentIDPattern.MatchString(resource.ID):
		return errors.New("page ID is not numeric")
	case !contentIDPattern.MatchString(resource.SpaceID):
		return errors.New("space ID is not numeric")
	case resource.Title == "" || !utf8.ValidString(resource.Title) || utf8.RuneCountInString(resource.Title) > 4*maximumTitleCharacters:
		return errors.New("page title is missing or invalid")
	case resource.Version == nil || resource.Version.Number < 1:
		return errors.New("page version is missing")
	}
	return nil
}

// validatePageTitle checks a title to write: one line of at most 255 characters.
func validatePageTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)
	switch {
	case trimmed == "":
		return "", errors.New("title is required")
	case !utf8.ValidString(trimmed):
		return "", errors.New("title must be valid UTF-8")
	case utf8.RuneCountInString(trimmed) > maximumTitleCharacters:
		return "", errors.New("title cannot exceed 255 characters")
	}
	for _, character := range trimmed {
		if character < ' ' || character == 0x7f {
			return "", errors.New("title must be one line without control characters")
		}
	}
	return trimmed, nil
}

func validateContentID(value string, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !contentIDPattern.MatchString(trimmed) {
		return "", errors.New(fieldName + " must be a numeric Confluence ID such as 123456")
	}
	return trimmed, nil
}

func pagePath(pageID string) string {
	return "/pages/" + url.PathEscape(pageID)
}

func safeAccountID(accountID string) string {
	if accountIDPattern.MatchString(accountID) {
		return accountID
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
