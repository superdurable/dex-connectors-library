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
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CreateReleaseOperation creates a GitLab project release.
type CreateReleaseOperation struct{ client *Client }

// GetReleaseOperation reads one project release.
type GetReleaseOperation struct{ client *Client }

// ListReleasesOperation reads one page of releases.
type ListReleasesOperation struct{ client *Client }

// UpdateReleaseOperation replaces selected release fields.
type UpdateReleaseOperation struct{ client *Client }

// DeleteReleaseOperation removes a release but keeps its tag.
type DeleteReleaseOperation struct{ client *Client }

// CreateRelease binds the create Mutation to this client.
func (client *Client) CreateRelease() CreateReleaseOperation { return CreateReleaseOperation{client} }

// GetRelease binds the get Query to this client.
func (client *Client) GetRelease() GetReleaseOperation { return GetReleaseOperation{client} }

// ListReleases binds the list Query to this client.
func (client *Client) ListReleases() ListReleasesOperation { return ListReleasesOperation{client} }

// UpdateRelease binds the update Mutation to this client.
func (client *Client) UpdateRelease() UpdateReleaseOperation { return UpdateReleaseOperation{client} }

// DeleteRelease binds the delete Mutation to this client.
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

// IdempotencyKey derives a stable Dex retry identity.
func (CreateReleaseOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateReleaseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable Dex retry identity.
func (UpdateReleaseOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateReleaseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable Dex retry identity.
func (DeleteReleaseOperation) IdempotencyKey(callID sdkgo.CallID, _ GetReleaseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a release and checks its unique tag before an uncertain resend.
func (operation CreateReleaseOperation) Invoke(call sdkgo.Call, input CreateReleaseInput) sdkgo.MutationAttempt[Release] {
	path, err := releasePath(GetReleaseInput{ProjectID: input.ProjectID, TagName: input.TagName})
	if err != nil {
		return limitedBranch(CreateReleaseBranchDefect, Release{}, "createRelease", sdkgo.FailureValidation, err.Error())
	}
	payload := map[string]string{"tag_name": input.TagName}
	if input.Name != "" {
		payload["name"] = input.Name
	}
	if input.Description != "" {
		payload["description"] = input.Description
	}
	if input.Ref != "" {
		payload["ref"] = input.Ref
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return limitedBranch(CreateReleaseBranchDefect, Release{}, "createRelease", sdkgo.FailureLocalDefect, "release request cannot be encoded")
	}
	request := limitedWrite[Release]{client: operation.client, operation: "createRelease", spec: requestSpec{method: http.MethodPost, path: strings.TrimSuffix(path, "/"+url.PathEscape(input.TagName)), body: body, contentType: "application/json"}, created: CreateReleaseBranchCreated, rejected: CreateReleaseBranchProviderRejected, defect: CreateReleaseBranchDefect, decode: decodeRelease, providerID: func(release Release) string { return release.TagName }, readBackPath: func(tagName string) string {
		return strings.TrimSuffix(path, "/"+url.PathEscape(input.TagName)) + "/" + url.PathEscape(tagName)
	}, naturalReadBackID: input.TagName, matchesReadBack: func(release Release) bool {
		return release.TagName == input.TagName && (input.Name == "" || release.Name == input.Name) && (input.Description == "" || release.Description == input.Description)
	}}
	return request.invoke(call)
}

// Invoke reads one release by its Git tag.
func (operation GetReleaseOperation) Invoke(call sdkgo.Call, input GetReleaseInput) sdkgo.QueryAttempt[Release] {
	path, err := releasePath(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetReleaseBranchDefect, Release{}, failure(sdkgo.FailureValidation, "getRelease", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "getRelease", requestSpec{method: http.MethodGet, path: path}, GetReleaseBranchFound, GetReleaseBranchNotFound, GetReleaseBranchProviderRejected, GetReleaseBranchInvalidResponse, GetReleaseBranchDefect, decodeRelease)
}

// Invoke lists one page of project releases.
func (operation ListReleasesOperation) Invoke(call sdkgo.Call, input ListReleasesInput) sdkgo.QueryAttempt[ReleasesPage] {
	if strings.TrimSpace(input.ProjectID) == "" || input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 {
		return sdkgo.NewQueryBranch(ListReleasesBranchDefect, ReleasesPage{}, failure(sdkgo.FailureValidation, "listReleases", "project ID, page, or perPage is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	if input.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(input.PerPage))
	}
	return executeQuery(operation.client, call, "listReleases", requestSpec{method: http.MethodGet, path: "/projects/" + url.PathEscape(input.ProjectID) + "/releases", query: query}, ListReleasesBranchListed, ListReleasesBranchNotFound, ListReleasesBranchProviderRejected, ListReleasesBranchInvalidResponse, ListReleasesBranchDefect, decodeReleasesPage)
}

// Invoke replaces the selected release title or notes.
func (operation UpdateReleaseOperation) Invoke(call sdkgo.Call, input UpdateReleaseInput) sdkgo.MutationAttempt[Release] {
	path, err := releasePath(GetReleaseInput{ProjectID: input.ProjectID, TagName: input.TagName})
	if err != nil || (input.Name == nil && input.Description == nil) || (input.Name != nil && strings.TrimSpace(*input.Name) == "") {
		return sdkgo.NewMutationBranch(UpdateReleaseBranchDefect, Release{}, failure(sdkgo.FailureValidation, "updateRelease", "release identity and valid replacement fields are required"), sdkgo.Receipt{})
	}
	payload := map[string]string{}
	if input.Name != nil {
		payload["name"] = *input.Name
	}
	if input.Description != nil {
		payload["description"] = *input.Description
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateReleaseBranchDefect, Release{}, failure(sdkgo.FailureLocalDefect, "updateRelease", "release update cannot be encoded"), sdkgo.Receipt{})
	}
	return executeMutation(operation.client, call, "updateRelease", requestSpec{method: http.MethodPut, path: path, body: body, contentType: "application/json"}, UpdateReleaseBranchUpdated, UpdateReleaseBranchNotFound, UpdateReleaseBranchProviderRejected, UpdateReleaseBranchInvalidResponse, UpdateReleaseBranchDefect, decodeRelease)
}

// Invoke removes a release while leaving its Git tag untouched.
func (operation DeleteReleaseOperation) Invoke(call sdkgo.Call, input GetReleaseInput) sdkgo.MutationAttempt[DeleteReleaseOutput] {
	path, err := releasePath(input)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteReleaseBranchDefect, DeleteReleaseOutput{}, failure(sdkgo.FailureValidation, "deleteRelease", err.Error()), sdkgo.Receipt{})
	}
	response, localFailure, err := operation.client.send(call, "deleteRelease", requestSpec{method: http.MethodDelete, path: path})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(DeleteReleaseBranchDefect, DeleteReleaseOutput{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeleteReleaseOutput](sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "gitlab", Operation: "deleteRelease", Message: "GitLab delete request failed"}, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, "deleteRelease")
		return sdkgo.NewMutationRetry[DeleteReleaseOutput](retry, after)
	}
	if response.StatusCode == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteReleaseBranchNotFound, DeleteReleaseOutput{}, failure(sdkgo.FailureNotFound, "deleteRelease", "release not found"), sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeleteReleaseBranchProviderRejected, DeleteReleaseOutput{}, failure(sdkgo.FailureProviderRejection, "deleteRelease", "GitLab rejected HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	if response.StatusCode == http.StatusOK {
		body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
		if readErr != nil {
			return sdkgo.NewMutationBranch(sdkgo.BranchID("invalidResponse"), DeleteReleaseOutput{}, failure(sdkgo.FailureProtocol, "deleteRelease", "release response could not be read"), sdkgo.Receipt{})
		}
		deleted, decodeErr := decodeRelease(body)
		if decodeErr != nil || deleted.TagName != input.TagName {
			return sdkgo.NewMutationBranch(sdkgo.BranchID("invalidResponse"), DeleteReleaseOutput{}, failure(sdkgo.FailureProtocol, "deleteRelease", "GitLab returned invalid release JSON"), sdkgo.Receipt{})
		}
	}
	return sdkgo.NewMutationBranch(DeleteReleaseBranchDeleted, DeleteReleaseOutput{ProjectID: input.ProjectID, TagName: input.TagName}, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "gitlab", ProviderObjectID: input.TagName})
}

func releasePath(input GetReleaseInput) (string, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.TagName) == "" || strings.ContainsAny(input.TagName, "\r\n") {
		return "", fmt.Errorf("project ID and Git tag are required")
	}
	return "/projects/" + url.PathEscape(input.ProjectID) + "/releases/" + url.PathEscape(input.TagName), nil
}

func decodeRelease(body []byte) (Release, error) {
	var value Release
	if err := json.Unmarshal(body, &value); err != nil {
		return Release{}, err
	}
	if value.TagName == "" {
		return Release{}, fmt.Errorf("missing release tag")
	}
	return value, nil
}

func decodeReleasesPage(body []byte) (ReleasesPage, error) {
	var releases []Release
	if err := json.Unmarshal(body, &releases); err != nil {
		return ReleasesPage{}, err
	}
	if releases == nil {
		return ReleasesPage{}, fmt.Errorf("missing releases")
	}
	for _, release := range releases {
		if release.TagName == "" {
			return ReleasesPage{}, fmt.Errorf("release tag is missing")
		}
	}
	return ReleasesPage{Releases: releases}, nil
}
