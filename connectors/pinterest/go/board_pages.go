// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinterest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListBoardPinsInput selects a page of Pins on one board.
type ListBoardPinsInput struct {
	// BoardID is the numeric Pinterest board ID.
	BoardID string `json:"boardId"`
	// PageSize limits the page to 1 through 250 Pins; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Bookmark continues from a prior board Pin page; blank starts at the first page.
	Bookmark string `json:"bookmark,omitempty"`
}

// ListBoardSectionsInput selects a page of sections on one board.
type ListBoardSectionsInput struct {
	// BoardID is the numeric Pinterest board ID.
	BoardID string `json:"boardId"`
	// PageSize limits the page to 1 through 250 sections; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Bookmark continues from a prior section page; blank starts at the first page.
	Bookmark string `json:"bookmark,omitempty"`
}

// ListBoardSectionPinsInput selects a page of Pins on one board section.
type ListBoardSectionPinsInput struct {
	// BoardID is the numeric Pinterest board ID.
	BoardID string `json:"boardId"`
	// SectionID is the numeric Pinterest section ID.
	SectionID string `json:"sectionId"`
	// PageSize limits the page to 1 through 250 Pins; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Bookmark continues from a prior section Pin page; blank starts at the first page.
	Bookmark string `json:"bookmark,omitempty"`
}

// BoardSection is one board section's identity and display name.
type BoardSection struct {
	// ID is the numeric Pinterest section ID.
	ID string `json:"id"`
	// Name is the section's display name.
	Name string `json:"name"`
}

// BoardSectionsPage holds one bounded page of sections and its cursor.
type BoardSectionsPage struct {
	// Sections are the identities on this page.
	Sections []BoardSection `json:"sections"`
	// NextBookmark is blank after the last page.
	NextBookmark string `json:"nextBookmark"`
}

// ListBoardPinsOperation retrieves one page of Pins on a board.
type ListBoardPinsOperation struct{ client *Client }

// ListBoardPins binds the board Pin Query to this client.
func (client *Client) ListBoardPins() ListBoardPinsOperation { return ListBoardPinsOperation{client} }

// Definition returns the generated board Pin page contract.
func (ListBoardPinsOperation) Definition() sdkgo.QueryDefinition { return ListBoardPinsDefinition }

// Invoke fetches one board Pin page.
func (operation ListBoardPinsOperation) Invoke(call sdkgo.Call, input ListBoardPinsInput) sdkgo.QueryAttempt[PinsPage] {
	if !numericID.MatchString(input.BoardID) || !validPageInput(input.PageSize, input.Bookmark) {
		return pinterestPageBranch[PinsPage]("listBoardPins", ListBoardPinsBranchDefect, sdkgo.FailureValidation, "board ID or page input is invalid")
	}
	pageSize := pageSizeOrDefault(input.PageSize)
	response := operation.client.get(call, "/v5/boards/"+input.BoardID+"/pins?"+pageQuery(pageSize, input.Bookmark))
	return pinterestPageAttempt("listBoardPins", response, ListBoardPinsBranchListed, ListBoardPinsBranchProviderRejected, ListBoardPinsBranchInvalidResponse, ListBoardPinsBranchDefect, func(body []byte) (PinsPage, error) { return decodePinsPage(body, pageSize) })
}

// ListBoardSectionsOperation retrieves one page of sections on a board.
type ListBoardSectionsOperation struct{ client *Client }

// ListBoardSections binds the section Query to this client.
func (client *Client) ListBoardSections() ListBoardSectionsOperation {
	return ListBoardSectionsOperation{client}
}

// Definition returns the generated board section page contract.
func (ListBoardSectionsOperation) Definition() sdkgo.QueryDefinition {
	return ListBoardSectionsDefinition
}

// Invoke fetches one board section page.
func (operation ListBoardSectionsOperation) Invoke(call sdkgo.Call, input ListBoardSectionsInput) sdkgo.QueryAttempt[BoardSectionsPage] {
	if !numericID.MatchString(input.BoardID) || !validPageInput(input.PageSize, input.Bookmark) {
		return pinterestPageBranch[BoardSectionsPage]("listBoardSections", ListBoardSectionsBranchDefect, sdkgo.FailureValidation, "board ID or page input is invalid")
	}
	pageSize := pageSizeOrDefault(input.PageSize)
	response := operation.client.get(call, "/v5/boards/"+input.BoardID+"/sections?"+pageQuery(pageSize, input.Bookmark))
	return pinterestPageAttempt("listBoardSections", response, ListBoardSectionsBranchListed, ListBoardSectionsBranchProviderRejected, ListBoardSectionsBranchInvalidResponse, ListBoardSectionsBranchDefect, func(body []byte) (BoardSectionsPage, error) { return decodeBoardSectionsPage(body, pageSize) })
}

// ListBoardSectionPinsOperation retrieves one page of Pins on a section.
type ListBoardSectionPinsOperation struct{ client *Client }

// ListBoardSectionPins binds the section Pin Query to this client.
func (client *Client) ListBoardSectionPins() ListBoardSectionPinsOperation {
	return ListBoardSectionPinsOperation{client}
}

// Definition returns the generated section Pin page contract.
func (ListBoardSectionPinsOperation) Definition() sdkgo.QueryDefinition {
	return ListBoardSectionPinsDefinition
}

// Invoke fetches one section Pin page.
func (operation ListBoardSectionPinsOperation) Invoke(call sdkgo.Call, input ListBoardSectionPinsInput) sdkgo.QueryAttempt[PinsPage] {
	if !numericID.MatchString(input.BoardID) || !numericID.MatchString(input.SectionID) || !validPageInput(input.PageSize, input.Bookmark) {
		return pinterestPageBranch[PinsPage]("listBoardSectionPins", ListBoardSectionPinsBranchDefect, sdkgo.FailureValidation, "board or section ID or page input is invalid")
	}
	pageSize := pageSizeOrDefault(input.PageSize)
	response := operation.client.get(call, "/v5/boards/"+input.BoardID+"/sections/"+input.SectionID+"/pins?"+pageQuery(pageSize, input.Bookmark))
	return pinterestPageAttempt("listBoardSectionPins", response, ListBoardSectionPinsBranchListed, ListBoardSectionPinsBranchProviderRejected, ListBoardSectionPinsBranchInvalidResponse, ListBoardSectionPinsBranchDefect, func(body []byte) (PinsPage, error) { return decodePinsPage(body, pageSize) })
}

func validPageInput(pageSize int, bookmark string) bool {
	return pageSize >= 0 && pageSize <= 250 && validBookmark(bookmark)
}

func pageSizeOrDefault(pageSize int) int {
	if pageSize == 0 {
		return 25
	}
	return pageSize
}

func pageQuery(pageSize int, bookmark string) string {
	query := url.Values{"page_size": {strconv.Itoa(pageSize)}}
	if bookmark != "" {
		query.Set("bookmark", bookmark)
	}
	return query.Encode()
}

func pinterestPageAttempt[T any](operationName string, response providerResponse, listed, rejected, invalidResponse, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	if errors.Is(response.err, errCredential) {
		return pinterestPageBranch[T](operationName, defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return pinterestPageRetry[T](operationName, sdkgo.FailureTransport, "Pinterest request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return pinterestPageRetry[T](operationName, sdkgo.FailureRateLimit, "Pinterest rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return pinterestPageRetry[T](operationName, sdkgo.FailureAvailability, "Pinterest is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return pinterestPageBranch[T](operationName, rejected, sdkgo.FailureProviderRejection, "Pinterest rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return pinterestPageBranch[T](operationName, invalidResponse, sdkgo.FailureResponseTooLarge, "Pinterest response exceeded the configured limit")
	}
	if response.err != nil {
		return pinterestPageRetry[T](operationName, sdkgo.FailureTransport, "Pinterest response was interrupted", 0)
	}
	value, err := decode(response.body)
	if err != nil {
		return pinterestPageBranch[T](operationName, invalidResponse, sdkgo.FailureProtocol, "Pinterest returned invalid page JSON")
	}
	return sdkgo.NewQueryBranch(listed, value, nil, sdkgo.Receipt{})
}

func decodeBoardSectionsPage(body []byte, pageSize int) (BoardSectionsPage, error) {
	var payload struct {
		Items    []BoardSection `json:"items"`
		Bookmark *string        `json:"bookmark"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return BoardSectionsPage{}, err
	}
	if payload.Items == nil || len(payload.Items) > pageSize {
		return BoardSectionsPage{}, fmt.Errorf("Pinterest board section page is invalid")
	}
	page := BoardSectionsPage{Sections: make([]BoardSection, 0, len(payload.Items))}
	if payload.Bookmark != nil {
		if !validBookmark(*payload.Bookmark) {
			return BoardSectionsPage{}, fmt.Errorf("Pinterest bookmark is invalid")
		}
		page.NextBookmark = *payload.Bookmark
	}
	for _, section := range payload.Items {
		if !numericID.MatchString(section.ID) || section.Name == "" {
			return BoardSectionsPage{}, fmt.Errorf("Pinterest board section identity is invalid")
		}
		page.Sections = append(page.Sections, section)
	}
	return page, nil
}

func pinterestPageBranch[T any](operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pinterest", Operation: operationName, Message: message}
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func pinterestPageRetry[T any](operationName string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "pinterest", Operation: operationName, Message: message}, delay)
}
