// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RepositoryTreeEntry is a GitLab file or directory reference.
type RepositoryTreeEntry struct {
	// ID is the Git object SHA.
	ID string `json:"id"`
	// Name is the entry's final path component.
	Name string `json:"name"`
	// Path is the repository-relative path.
	Path string `json:"path"`
	// Type is blob or tree.
	Type string `json:"type"`
}

// RepositoryTreePage contains one page of repository entries.
type RepositoryTreePage struct {
	// Entries contains the returned files and directories.
	Entries []RepositoryTreeEntry `json:"entries"`
}

// ListRepositoryTreeInput selects a project path and page.
type ListRepositoryTreeInput struct {
	// ProjectID is the numeric ID or namespace path.
	ProjectID string `json:"projectId"`
	// Path is an optional repository-relative directory; blank lists the root.
	Path string `json:"path,omitempty"`
	// Ref is an optional branch or tag; blank uses the default branch.
	Ref string `json:"ref,omitempty"`
	// Page is one-based; zero uses the first page.
	Page int `json:"page,omitempty"`
	// PerPage is at most 100; zero uses GitLab's default.
	PerPage int `json:"perPage,omitempty"`
}

// UserProjectsPage contains one page of a user's personal projects.
type UserProjectsPage struct {
	// Projects contains the returned projects.
	Projects []Project `json:"projects"`
}

// ListUserProjectsInput selects a GitLab user and page.
type ListUserProjectsInput struct {
	// UserID is the user's numeric ID or username.
	UserID string `json:"userId"`
	// Page is one-based; zero uses the first page.
	Page int `json:"page,omitempty"`
	// PerPage is at most 100; zero uses GitLab's default.
	PerPage int `json:"perPage,omitempty"`
}

// ListRepositoryTreeOperation lists repository entries.
type ListRepositoryTreeOperation struct{ client *Client }

// ListUserProjectsOperation lists a user's personal projects.
type ListUserProjectsOperation struct{ client *Client }

// ListRepositoryTree binds the repository tree query to this client.
func (client *Client) ListRepositoryTree() ListRepositoryTreeOperation {
	return ListRepositoryTreeOperation{client}
}

// ListUserProjects binds the user project query to this client.
func (client *Client) ListUserProjects() ListUserProjectsOperation {
	return ListUserProjectsOperation{client}
}

// Definition returns the generated repository tree contract.
func (ListRepositoryTreeOperation) Definition() sdkgo.QueryDefinition {
	return ListRepositoryTreeDefinition
}

// Definition returns the generated user project contract.
func (ListUserProjectsOperation) Definition() sdkgo.QueryDefinition {
	return ListUserProjectsDefinition
}

// Invoke lists one repository tree page without returning file content.
func (operation ListRepositoryTreeOperation) Invoke(call sdkgo.Call, input ListRepositoryTreeInput) sdkgo.QueryAttempt[RepositoryTreePage] {
	if strings.TrimSpace(input.ProjectID) == "" || input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 || strings.HasPrefix(input.Path, "/") || strings.Contains(input.Path, "..") {
		return sdkgo.NewQueryBranch(ListRepositoryTreeBranchDefect, RepositoryTreePage{}, failure(sdkgo.FailureValidation, "listRepositoryTree", "project, path, or page is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{}
	if input.Path != "" {
		query.Set("path", input.Path)
	}
	if input.Ref != "" {
		query.Set("ref", input.Ref)
	}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	if input.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(input.PerPage))
	}
	return executeQuery(operation.client, call, "listRepositoryTree", requestSpec{method: http.MethodGet, path: "/projects/" + url.PathEscape(input.ProjectID) + "/repository/tree", query: query}, ListRepositoryTreeBranchListed, ListRepositoryTreeBranchNotFound, ListRepositoryTreeBranchProviderRejected, ListRepositoryTreeBranchInvalidResponse, ListRepositoryTreeBranchDefect, decodeRepositoryTree)
}

// Invoke lists one user's personal project page.
func (operation ListUserProjectsOperation) Invoke(call sdkgo.Call, input ListUserProjectsInput) sdkgo.QueryAttempt[UserProjectsPage] {
	if strings.TrimSpace(input.UserID) == "" || input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 {
		return sdkgo.NewQueryBranch(ListUserProjectsBranchDefect, UserProjectsPage{}, failure(sdkgo.FailureValidation, "listUserProjects", "user ID or page is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	if input.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(input.PerPage))
	}
	return executeQuery(operation.client, call, "listUserProjects", requestSpec{method: http.MethodGet, path: "/users/" + url.PathEscape(input.UserID) + "/projects", query: query}, ListUserProjectsBranchListed, ListUserProjectsBranchNotFound, ListUserProjectsBranchProviderRejected, ListUserProjectsBranchInvalidResponse, ListUserProjectsBranchDefect, decodeUserProjects)
}

func decodeRepositoryTree(body []byte) (RepositoryTreePage, error) {
	var entries []RepositoryTreeEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return RepositoryTreePage{}, err
	}
	if entries == nil {
		return RepositoryTreePage{}, fmt.Errorf("missing repository tree")
	}
	return RepositoryTreePage{Entries: entries}, nil
}

func decodeUserProjects(body []byte) (UserProjectsPage, error) {
	var projects []Project
	if err := json.Unmarshal(body, &projects); err != nil {
		return UserProjectsPage{}, err
	}
	if projects == nil {
		return UserProjectsPage{}, fmt.Errorf("missing user projects")
	}
	return UserProjectsPage{Projects: projects}, nil
}
