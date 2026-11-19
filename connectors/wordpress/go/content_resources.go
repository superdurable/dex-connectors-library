// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdatePostInput replaces a post's editorial fields.
type UpdatePostInput struct {
	// PostID identifies the post.
	PostID int64 `json:"postId"`
	// Title is the replacement title, which may contain HTML.
	Title string `json:"title"`
	// Content is the replacement body, which may contain HTML.
	Content string `json:"content"`
	// Status is draft or publish.
	Status string `json:"status"`
}

// DeletePostInput identifies a post to move to Trash.
type DeletePostInput struct {
	// PostID identifies the post.
	PostID int64 `json:"postId"`
}

// Page is a WordPress page summary with untrusted rendered HTML.
type Page struct {
	// ID is the stable numeric page ID.
	ID int64 `json:"id"`
	// Slug is the mutable URL slug.
	Slug string `json:"slug"`
	// Status is the current publication status.
	Status string `json:"status"`
	// Link is the public page URL when published.
	Link string `json:"link"`
	// Title contains untrusted rendered HTML.
	Title RenderedText `json:"title"`
}

// PagePage contains one page of provider pages.
type PagePage struct {
	// Pages contains the returned pages, possibly none.
	Pages []Page `json:"pages"`
}

// CreatePageInput describes a new page.
type CreatePageInput struct {
	// Title is the page title.
	Title string `json:"title"`
	// Content is the page body.
	Content string `json:"content"`
	// Status is draft or publish; blank uses draft.
	Status string `json:"status,omitempty"`
}

// GetPageInput identifies one page.
type GetPageInput struct {
	// PageID identifies the page.
	PageID int64 `json:"pageId"`
}

// ListPagesInput selects a page of pages.
type ListPagesInput struct {
	// Page is one-based; zero uses the provider default.
	Page int `json:"page,omitempty"`
	// PerPage is at most 100; zero uses the provider default.
	PerPage int `json:"perPage,omitempty"`
}

// UpdatePageInput replaces a page's editorial fields.
type UpdatePageInput struct {
	// PageID identifies the page.
	PageID int64 `json:"pageId"`
	// Title is the replacement title.
	Title string `json:"title"`
	// Content is the replacement body.
	Content string `json:"content"`
	// Status is draft or publish.
	Status string `json:"status"`
}

// DeletePageInput identifies a page to move to Trash.
type DeletePageInput struct {
	// PageID identifies the page.
	PageID int64 `json:"pageId"`
}

// User is a public WordPress user summary; it never contains a password.
type User struct {
	// ID is the stable numeric user ID.
	ID int64 `json:"id"`
	// Name is the displayed user name.
	Name string `json:"name"`
	// Slug is the public author slug.
	Slug string `json:"slug"`
}

// UserPage contains one page of users.
type UserPage struct {
	// Users contains the returned users, possibly none.
	Users []User `json:"users"`
}

// GetUserInput identifies one user.
type GetUserInput struct {
	// UserID identifies the user.
	UserID int64 `json:"userId"`
}

// ListUsersInput selects a user page.
type ListUsersInput struct {
	// Page is one-based; zero uses the provider default.
	Page int `json:"page,omitempty"`
	// PerPage is at most 100; zero uses the provider default.
	PerPage int `json:"perPage,omitempty"`
}

// UpdateUserInput replaces one user's display name.
type UpdateUserInput struct {
	// UserID identifies the user.
	UserID int64 `json:"userId"`
	// Name is the replacement display name.
	Name string `json:"name"`
}

func decodeContentItem[T any](body []byte) (T, error) {
	var value T
	if err := json.Unmarshal(body, &value); err != nil {
		return value, err
	}
	return value, nil
}

func decodeContentPage[T any](body []byte) ([]T, error) {
	items, err := decodeContentItem[[]T](body)
	if err != nil || items == nil {
		return nil, fmt.Errorf("invalid resource page")
	}
	return items, nil
}

func contentQuery(page, perPage int) (url.Values, bool) {
	if page < 0 || perPage < 0 || perPage > 100 {
		return nil, false
	}
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		query.Set("per_page", strconv.Itoa(perPage))
	}
	return query, true
}

// GetPageOperation reads one page.
type GetPageOperation struct{ client *Client }

// GetPage returns the page lookup Query.
func (client *Client) GetPage() GetPageOperation { return GetPageOperation{client} }

// Definition returns the generated getPage contract.
func (GetPageOperation) Definition() sdkgo.QueryDefinition { return GetPageDefinition }

// Invoke reads one page by ID.
func (operation GetPageOperation) Invoke(call sdkgo.Call, input GetPageInput) sdkgo.QueryAttempt[Page] {
	if input.PageID <= 0 {
		return failedQuery[Page]("getPage", GetPageBranchDefect, sdkgo.FailureValidation, "pageId must be positive")
	}
	return invokeWordPressQuery(operation.client, call, "getPage", "/wp-json/wp/v2/pages/"+strconv.FormatInt(input.PageID, 10), nil,
		queryBranches{found: GetPageBranchFound, notFound: GetPageBranchNotFound, rejected: GetPageBranchProviderRejected, invalid: GetPageBranchInvalidResponse, defect: GetPageBranchDefect},
		func(body []byte, _ http.Header) (Page, error) {
			page, err := decodeContentItem[Page](body)
			if err != nil || page.ID <= 0 {
				return Page{}, fmt.Errorf("invalid page")
			}
			return page, nil
		})
}

// ListPagesOperation reads one page of pages.
type ListPagesOperation struct{ client *Client }

// ListPages returns the page list Query.
func (client *Client) ListPages() ListPagesOperation { return ListPagesOperation{client} }

// Definition returns the generated listPages contract.
func (ListPagesOperation) Definition() sdkgo.QueryDefinition { return ListPagesDefinition }

// Invoke reads a bounded page of pages.
func (operation ListPagesOperation) Invoke(call sdkgo.Call, input ListPagesInput) sdkgo.QueryAttempt[PagePage] {
	query, valid := contentQuery(input.Page, input.PerPage)
	if !valid {
		return failedQuery[PagePage]("listPages", ListPagesBranchDefect, sdkgo.FailureValidation, "page and perPage are invalid")
	}
	return invokeWordPressQuery(operation.client, call, "listPages", "/wp-json/wp/v2/pages", query,
		queryBranches{found: ListPagesBranchListed, notFound: ListPagesBranchNotFound, rejected: ListPagesBranchProviderRejected, invalid: ListPagesBranchInvalidResponse, defect: ListPagesBranchDefect},
		func(body []byte, _ http.Header) (PagePage, error) {
			pages, err := decodeContentPage[Page](body)
			return PagePage{Pages: pages}, err
		})
}

// GetUserOperation reads one user.
type GetUserOperation struct{ client *Client }

// GetUser returns the user lookup Query.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client} }

// Definition returns the generated getUser contract.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Invoke reads one user by ID.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[User] {
	if input.UserID <= 0 {
		return failedQuery[User]("getUser", GetUserBranchDefect, sdkgo.FailureValidation, "userId must be positive")
	}
	return invokeWordPressQuery(operation.client, call, "getUser", "/wp-json/wp/v2/users/"+strconv.FormatInt(input.UserID, 10), nil,
		queryBranches{found: GetUserBranchFound, notFound: GetUserBranchNotFound, rejected: GetUserBranchProviderRejected, invalid: GetUserBranchInvalidResponse, defect: GetUserBranchDefect},
		func(body []byte, _ http.Header) (User, error) {
			user, err := decodeContentItem[User](body)
			if err != nil || user.ID <= 0 {
				return User{}, fmt.Errorf("invalid user")
			}
			return user, nil
		})
}

// ListUsersOperation reads one user page.
type ListUsersOperation struct{ client *Client }

// ListUsers returns the user list Query.
func (client *Client) ListUsers() ListUsersOperation { return ListUsersOperation{client} }

// Definition returns the generated listUsers contract.
func (ListUsersOperation) Definition() sdkgo.QueryDefinition { return ListUsersDefinition }

// Invoke reads a bounded user page.
func (operation ListUsersOperation) Invoke(call sdkgo.Call, input ListUsersInput) sdkgo.QueryAttempt[UserPage] {
	query, valid := contentQuery(input.Page, input.PerPage)
	if !valid {
		return failedQuery[UserPage]("listUsers", ListUsersBranchDefect, sdkgo.FailureValidation, "page and perPage are invalid")
	}
	return invokeWordPressQuery(operation.client, call, "listUsers", "/wp-json/wp/v2/users", query,
		queryBranches{found: ListUsersBranchListed, notFound: ListUsersBranchNotFound, rejected: ListUsersBranchProviderRejected, invalid: ListUsersBranchInvalidResponse, defect: ListUsersBranchDefect},
		func(body []byte, _ http.Header) (UserPage, error) {
			users, err := decodeContentPage[User](body)
			return UserPage{Users: users}, err
		})
}
