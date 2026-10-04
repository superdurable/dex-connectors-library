// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListReleasesInput selects a single bounded page; date filtering is performed by the caller.
type ListReleasesInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner.
	Repository string `json:"repository"`
	// PageSize is 1 through 100. Zero selects 30.
	PageSize int `json:"pageSize,omitempty"`
	// Page is one-based. Zero selects 1.
	Page int `json:"page,omitempty"`
	// MaxBodyCharacters bounds each release's notes to 1 through 16384 Unicode characters. Zero selects 4000.
	MaxBodyCharacters int `json:"maxBodyCharacters,omitempty"`
}

// ReleasePage contains one page in provider order, which is not necessarily publication-date order.
type ReleasePage struct {
	// Releases holds the page, including drafts visible to the grant and prereleases.
	Releases []Release `json:"releases"`
	// NextPage is the following page number, or zero when GitHub provides no next link.
	NextPage int `json:"nextPage"`
}

// Release contains provider identity and bounded release notes, never credentials or asset contents.
type Release struct {
	// ID is the stable GitHub release ID; a tag or title can be edited without changing it.
	ID int64 `json:"id"`
	// TagName is the release's tag, bounded to 255 bytes.
	TagName string `json:"tagName"`
	// Name is the optional release title, bounded to 1024 bytes.
	Name string `json:"name,omitempty"`
	// URL is the safe public web URL, or empty if GitHub returned an unsafe URL.
	URL string `json:"url,omitempty"`
	// Body is the beginning of the release notes, limited by MaxBodyCharacters.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports whether the notes were shortened.
	BodyTruncated bool `json:"bodyTruncated"`
	// IsDraft reports an unpublished draft visible to this grant.
	IsDraft bool `json:"isDraft"`
	// IsPrerelease reports GitHub's prerelease flag.
	IsPrerelease bool `json:"isPrerelease"`
	// CreatedAt is GitHub's creation timestamp; it must not be used as publication time.
	CreatedAt time.Time `json:"createdAt"`
	// PublishedAt is the publication time in UTC, absent for an unpublished draft.
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// ListReleasesOperation implements the read-only listReleases query.
type ListReleasesOperation struct{ client *Client }

// Definition returns the manifest-generated execution and branch contract.
func (ListReleasesOperation) Definition() sdkgo.QueryDefinition { return ListReleasesDefinition }

type githubRelease struct {
	ID           int64      `json:"id"`
	TagName      string     `json:"tag_name"`
	Name         string     `json:"name"`
	HTMLURL      string     `json:"html_url"`
	Body         string     `json:"body"`
	IsDraft      bool       `json:"draft"`
	IsPrerelease bool       `json:"prerelease"`
	CreatedAt    time.Time  `json:"created_at"`
	PublishedAt  *time.Time `json:"published_at"`
}

// Invoke performs one authenticated GET. Transport/rate-limit recovery follows the query's manifest policy.
func (operation ListReleasesOperation) Invoke(call sdkgo.Call, input ListReleasesInput) sdkgo.QueryAttempt[ReleasePage] {
	const operationID = "listReleases"
	branches := changeBranches{responseBranches: responseBranches{
		insufficientScope: ListReleasesBranchInsufficientScope, revoked: ListReleasesBranchAuthorizationRevoked,
		notFound: ListReleasesBranchNotFound, providerRejected: ListReleasesBranchProviderRejected,
	}, invalidResponse: ListReleasesBranchInvalidResponse, defect: ListReleasesBranchDefect}
	owner, repository, failure := validateRepository(operationID, input.Owner, input.Repository)
	pageSize, page := input.PageSize, input.Page
	if failure == nil {
		pageSize, page, failure = validatePage(operationID, pageSize, page, 30)
	}
	maximumBodyCharacters := input.MaxBodyCharacters
	if maximumBodyCharacters == 0 {
		maximumBodyCharacters = 4000
	}
	if maximumBodyCharacters < 1 || maximumBodyCharacters > 16384 {
		invalid := providerFailure(operationID, sdkgo.FailureValidation, "maxBodyCharacters must be between 1 and 16384")
		failure = &invalid
	}
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, ReleasePage{}, failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, ReleasePage{}, failure, sdkgo.Receipt{})
	}
	query := url.Values{"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/releases"
	response, attempt := fetchChangePage[ReleasePage](operation.client, call, credential, operationID, path, query, branches)
	if attempt != nil {
		return *attempt
	}
	if attempt := acceptChangeResponse[ReleasePage](operation.client, operationID, response, branches); attempt != nil {
		return *attempt
	}
	var providerReleases []githubRelease
	if err := decodeJSON(response.body, &providerReleases); err != nil || providerReleases == nil || len(providerReleases) > pageSize {
		return invalidChangeResponse[ReleasePage](operationID, "GitHub returned an invalid release page", response, branches)
	}
	releases := make([]Release, 0, len(providerReleases))
	for _, release := range providerReleases {
		if release.ID <= 0 || !isValidQueryText(release.TagName, maximumRefBytes, false) || strings.TrimSpace(release.TagName) == "" || release.CreatedAt.IsZero() || (!release.IsDraft && (release.PublishedAt == nil || release.PublishedAt.IsZero())) {
			return invalidChangeResponse[ReleasePage](operationID, "GitHub returned an invalid release", response, branches)
		}
		body, isTruncated := truncateCharacters(release.Body, maximumBodyCharacters)
		var publishedAt *time.Time
		if release.PublishedAt != nil {
			value := release.PublishedAt.UTC()
			publishedAt = &value
		}
		releases = append(releases, Release{ID: release.ID, TagName: release.TagName,
			Name: boundedString(release.Name, maximumTitleBytes), URL: safeURL(release.HTMLURL), Body: body, BodyTruncated: isTruncated,
			IsDraft: release.IsDraft, IsPrerelease: release.IsPrerelease, CreatedAt: release.CreatedAt.UTC(), PublishedAt: publishedAt})
	}
	nextPage, isValidLink := nextPageNumber(response.header, page)
	if !isValidLink {
		return invalidChangeResponse[ReleasePage](operationID, "GitHub returned an invalid pagination link", response, branches)
	}
	return sdkgo.NewQueryBranch(ListReleasesBranchListed, ReleasePage{Releases: releases, NextPage: nextPage}, nil, receipt(response))
}
