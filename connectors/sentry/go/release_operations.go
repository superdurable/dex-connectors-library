// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sentry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Release contains the version and selected metadata of a Sentry release.
type Release struct {
	// Version is the release identifier unique in the organization.
	Version string `json:"version"`
	// Status is open or archived.
	Status string `json:"status"`
	// Ref is the associated commit reference when present.
	Ref string `json:"ref,omitempty"`
	// URL points to the source release when present.
	URL string `json:"url,omitempty"`
	// Projects lists projects associated with this version.
	Projects []ReleaseProject `json:"projects,omitempty"`
}

// ReleaseProject identifies an associated Sentry project.
type ReleaseProject struct {
	// Slug is the project slug.
	Slug string `json:"slug"`
}

// ReleasePage contains one provider page of releases.
type ReleasePage struct {
	// Releases contains one page of releases.
	Releases []Release `json:"releases"`
}

// CreateReleaseInput creates a version for one project.
type CreateReleaseInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Project is the project slug.
	Project string `json:"project"`
	// Version is the new release version.
	Version string `json:"version"`
}

// GetReleaseInput selects one release by version.
type GetReleaseInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Version is the release version.
	Version string `json:"version"`
}

// ListReleasesInput selects a release page.
type ListReleasesInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Cursor is Sentry's opaque page cursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Query filters versions by a case-insensitive substring.
	Query string `json:"query,omitempty"`
}

// UpdateReleaseInput sets absolute release metadata by version.
type UpdateReleaseInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Version is the release version.
	Version string `json:"version"`
	// Status is open or archived.
	Status string `json:"status"`
	// Ref sets an optional commit reference when nonempty.
	Ref string `json:"ref,omitempty"`
	// URL sets an optional release URL when nonempty.
	URL string `json:"url,omitempty"`
}

// DeleteReleaseInput selects one release for permanent deletion.
type DeleteReleaseInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Version is the release version.
	Version string `json:"version"`
}

// DeletedRelease confirms accepted deletion of one version.
type DeletedRelease struct {
	// Version is the deleted release version.
	Version string `json:"version"`
}

// CreateReleaseOperation creates a release with bounded resend.
type CreateReleaseOperation struct{ client *Client }

// GetReleaseOperation retrieves one release.
type GetReleaseOperation struct{ client *Client }

// ListReleasesOperation lists releases.
type ListReleasesOperation struct{ client *Client }

// UpdateReleaseOperation changes a release's metadata.
type UpdateReleaseOperation struct{ client *Client }

// DeleteReleaseOperation deletes a release.
type DeleteReleaseOperation struct{ client *Client }

// CreateRelease binds release creation to this client.
func (client *Client) CreateRelease() CreateReleaseOperation { return CreateReleaseOperation{client} }

// GetRelease binds release retrieval to this client.
func (client *Client) GetRelease() GetReleaseOperation { return GetReleaseOperation{client} }

// ListReleases binds release listing to this client.
func (client *Client) ListReleases() ListReleasesOperation { return ListReleasesOperation{client} }

// UpdateRelease binds release updates to this client.
func (client *Client) UpdateRelease() UpdateReleaseOperation { return UpdateReleaseOperation{client} }

// DeleteRelease binds release deletion to this client.
func (client *Client) DeleteRelease() DeleteReleaseOperation { return DeleteReleaseOperation{client} }

// Definition returns the generated createRelease contract.
func (CreateReleaseOperation) Definition() sdkgo.MutationDefinition { return CreateReleaseDefinition }

// Definition returns the generated getRelease contract.
func (GetReleaseOperation) Definition() sdkgo.QueryDefinition { return GetReleaseDefinition }

// Definition returns the generated listReleases contract.
func (ListReleasesOperation) Definition() sdkgo.QueryDefinition { return ListReleasesDefinition }

// Definition returns the generated updateRelease contract.
func (UpdateReleaseOperation) Definition() sdkgo.MutationDefinition { return UpdateReleaseDefinition }

// Definition returns the generated deleteRelease contract.
func (DeleteReleaseOperation) Definition() sdkgo.MutationDefinition { return DeleteReleaseDefinition }

// IdempotencyKey correlates possible create sends.
func (CreateReleaseOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateReleaseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute updates.
func (UpdateReleaseOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateReleaseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates deletion attempts.
func (DeleteReleaseOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteReleaseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a release and reads back unknown outcomes by version.
func (operation CreateReleaseOperation) Invoke(call sdkgo.Call, input CreateReleaseInput) sdkgo.MutationAttempt[Release] {
	if !validSentrySegments(input.Organization, input.Project, input.Version) {
		return mutationFailure[Release]("createRelease", CreateReleaseBranchDefect, sdkgo.FailureValidation, "organization, project, and version are required")
	}
	body, err := json.Marshal(struct {
		Version  string   `json:"version"`
		Projects []string `json:"projects"`
	}{input.Version, []string{input.Project}})
	if err != nil {
		return mutationFailure[Release]("createRelease", CreateReleaseBranchDefect, sdkgo.FailureValidation, "release cannot be encoded")
	}
	request := sentryLimitedCreate[Release]{client: operation.client, call: call, operation: "createRelease", path: releaseListPath(input.Organization), body: body, timeout: CreateReleaseDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateReleaseBranchCreated, rejected: CreateReleaseBranchProviderRejected, defect: CreateReleaseBranchDefect, decode: func(body []byte) (Release, error) {
		return decodeReleaseVersion(body, input.Version)
	}, readBack: func(call sdkgo.Call) (Release, readBackState) { return operation.readBackRelease(call, input) }}
	return request.invoke()
}

// Invoke retrieves one release.
func (operation GetReleaseOperation) Invoke(call sdkgo.Call, input GetReleaseInput) sdkgo.QueryAttempt[Release] {
	if !validSentrySegments(input.Organization, input.Version) {
		return queryFailure[Release]("getRelease", GetReleaseBranchDefect, sdkgo.FailureValidation, "organization and version are required")
	}
	return runQuery(operation.client, call, "getRelease", releasePath(input.Organization, input.Version), GetReleaseBranchFound, GetReleaseBranchNotFound, GetReleaseBranchProviderRejected, GetReleaseBranchInvalidResponse, GetReleaseBranchDefect, func(body []byte) (Release, error) { return decodeReleaseVersion(body, input.Version) })
}

// Invoke lists releases with an optional cursor and version query.
func (operation ListReleasesOperation) Invoke(call sdkgo.Call, input ListReleasesInput) sdkgo.QueryAttempt[ReleasePage] {
	if !validSentrySegments(input.Organization) {
		return queryFailure[ReleasePage]("listReleases", ListReleasesBranchDefect, sdkgo.FailureValidation, "organization is required")
	}
	path := releaseListPath(input.Organization)
	values := url.Values{}
	if input.Cursor != "" {
		values.Set("cursor", input.Cursor)
	}
	if input.Query != "" {
		values.Set("query", input.Query)
	}
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	return runQuery(operation.client, call, "listReleases", path, ListReleasesBranchListed, "", ListReleasesBranchProviderRejected, ListReleasesBranchInvalidResponse, ListReleasesBranchDefect, decodeReleasePage)
}

// Invoke replaces selected release metadata and status.
func (operation UpdateReleaseOperation) Invoke(call sdkgo.Call, input UpdateReleaseInput) sdkgo.MutationAttempt[Release] {
	if !validSentrySegments(input.Organization, input.Version) || (input.Status != "open" && input.Status != "archived") {
		return mutationFailure[Release]("updateRelease", UpdateReleaseBranchDefect, sdkgo.FailureValidation, "organization, version, and open or archived status are required")
	}
	body, err := json.Marshal(struct {
		Status string `json:"status"`
		Ref    string `json:"ref,omitempty"`
		URL    string `json:"url,omitempty"`
	}{input.Status, input.Ref, input.URL})
	if err != nil {
		return mutationFailure[Release]("updateRelease", UpdateReleaseBranchDefect, sdkgo.FailureValidation, "release update cannot be encoded")
	}
	return runMutation(operation.client, call, "updateRelease", http.MethodPut, releasePath(input.Organization, input.Version), body, "", "", UpdateReleaseBranchUpdated, UpdateReleaseBranchNotFound, UpdateReleaseBranchProviderRejected, UpdateReleaseBranchInvalidResponse, UpdateReleaseBranchDefect, func(body []byte) (Release, error) { return decodeReleaseVersion(body, input.Version) })
}

// Invoke permanently deletes one release.
func (operation DeleteReleaseOperation) Invoke(call sdkgo.Call, input DeleteReleaseInput) sdkgo.MutationAttempt[DeletedRelease] {
	if !validSentrySegments(input.Organization, input.Version) {
		return mutationFailure[DeletedRelease]("deleteRelease", DeleteReleaseBranchDefect, sdkgo.FailureValidation, "organization and version are required")
	}
	return runMutation(operation.client, call, "deleteRelease", http.MethodDelete, releasePath(input.Organization, input.Version), nil, "", "", DeleteReleaseBranchDeleted, DeleteReleaseBranchNotFound, DeleteReleaseBranchProviderRejected, DeleteReleaseBranchInvalidResponse, DeleteReleaseBranchDefect, func(body []byte) (DeletedRelease, error) {
		if len(body) != 0 {
			return DeletedRelease{}, fmt.Errorf("unexpected release deletion content")
		}
		return DeletedRelease{Version: input.Version}, nil
	})
}

func (operation CreateReleaseOperation) readBackRelease(call sdkgo.Call, input CreateReleaseInput) (Release, readBackState) {
	body, state := operation.client.readBackResource(call, releasePath(input.Organization, input.Version))
	if state != readBackMatched {
		return Release{}, state
	}
	release, err := decodeRelease(body)
	if err != nil {
		return Release{}, readBackRetry
	}
	if release.Version != input.Version {
		return Release{}, readBackConflicted
	}
	for _, project := range release.Projects {
		if project.Slug == input.Project {
			return release, readBackMatched
		}
	}
	return Release{}, readBackConflicted
}

func decodeReleaseVersion(body []byte, version string) (Release, error) {
	release, err := decodeRelease(body)
	if err == nil && release.Version != version {
		return Release{}, fmt.Errorf("Sentry returned a different release version")
	}
	return release, err
}

func decodeRelease(body []byte) (Release, error) {
	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return Release{}, err
	}
	if strings.TrimSpace(release.Version) == "" {
		return Release{}, fmt.Errorf("release version is missing")
	}
	return release, nil
}
func decodeReleasePage(body []byte) (ReleasePage, error) {
	var releases []Release
	if err := json.Unmarshal(body, &releases); err != nil {
		return ReleasePage{}, err
	}
	for _, release := range releases {
		if strings.TrimSpace(release.Version) == "" {
			return ReleasePage{}, fmt.Errorf("release page contains an invalid version")
		}
	}
	return ReleasePage{Releases: releases}, nil
}
func releaseListPath(organization string) string {
	return "/organizations/" + url.PathEscape(organization) + "/releases/"
}
func releasePath(organization, version string) string {
	return releaseListPath(organization) + url.PathEscape(version) + "/"
}
