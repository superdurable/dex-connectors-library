// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlab

// Release is the public identity and content of a GitLab project release.
type Release struct {
	// TagName is the associated Git tag.
	TagName string `json:"tag_name"`
	// Name is the displayed release title.
	Name string `json:"name"`
	// Description is the release notes in Markdown.
	Description string `json:"description"`
}

// ReleasesPage contains one page of project releases.
type ReleasesPage struct {
	// Releases contains the returned releases, possibly empty.
	Releases []Release `json:"releases"`
}

// CreateReleaseInput names a tag and optional source ref for a new release.
type CreateReleaseInput struct {
	// ProjectID is a numeric project ID or namespace path.
	ProjectID string `json:"projectId"`
	// TagName is the Git tag, such as v1.2.0.
	TagName string `json:"tagName"`
	// Name is the optional displayed title.
	Name string `json:"name,omitempty"`
	// Description is optional Markdown release notes.
	Description string `json:"description,omitempty"`
	// Ref is a branch, tag, or commit SHA used when TagName does not exist.
	Ref string `json:"ref,omitempty"`
}

// GetReleaseInput selects a project release by Git tag.
type GetReleaseInput struct {
	// ProjectID is a numeric project ID or namespace path.
	ProjectID string `json:"projectId"`
	// TagName is the associated Git tag.
	TagName string `json:"tagName"`
}

// ListReleasesInput selects one page of project releases.
type ListReleasesInput struct {
	// ProjectID is a numeric project ID or namespace path.
	ProjectID string `json:"projectId"`
	// Page is one-based; zero uses the first page.
	Page int `json:"page,omitempty"`
	// PerPage is at most 100; zero uses GitLab's default.
	PerPage int `json:"perPage,omitempty"`
}

// UpdateReleaseInput replaces selected release fields.
type UpdateReleaseInput struct {
	// ProjectID is a numeric project ID or namespace path.
	ProjectID string `json:"projectId"`
	// TagName is the associated Git tag.
	TagName string `json:"tagName"`
	// Name replaces the title when present.
	Name *string `json:"name,omitempty"`
	// Description replaces Markdown notes when present, including empty to clear them.
	Description *string `json:"description,omitempty"`
}

// DeleteReleaseOutput identifies the deleted release without its former content.
type DeleteReleaseOutput struct {
	// ProjectID is the project used for the request.
	ProjectID string `json:"projectId"`
	// TagName is the associated Git tag, which remains after deletion.
	TagName string `json:"tagName"`
}
