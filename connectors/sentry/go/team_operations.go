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

// Team contains selected Sentry team metadata.
type Team struct {
	// ID is the stable Sentry team ID.
	ID string `json:"id"`
	// Slug is the team's organization-local URL identifier.
	Slug string `json:"slug"`
	// Name is the team's display name.
	Name string `json:"name"`
}

// TeamPage contains one provider page of teams.
type TeamPage struct {
	// Teams contains the returned teams.
	Teams []Team `json:"teams"`
}

// CreateTeamInput creates a team with a stable slug.
type CreateTeamInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Slug is the new team slug.
	Slug string `json:"slug"`
	// Name is the new team's display name.
	Name string `json:"name"`
}

// GetTeamInput selects one team.
type GetTeamInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Slug is the team slug.
	Slug string `json:"slug"`
}

// ListTeamsInput selects one team page.
type ListTeamsInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Cursor is Sentry's opaque page cursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Query filters teams by name or slug.
	Query string `json:"query,omitempty"`
}

// UpdateTeamInput replaces a team's slug and name.
type UpdateTeamInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Slug is the current team slug.
	Slug string `json:"slug"`
	// NewSlug is the desired team slug.
	NewSlug string `json:"newSlug"`
	// Name is the desired display name.
	Name string `json:"name"`
}

// DeleteTeamInput selects a team for asynchronous deletion.
type DeleteTeamInput struct {
	// Organization is the organization slug.
	Organization string `json:"organization"`
	// Slug is the team slug.
	Slug string `json:"slug"`
}

// DeletedTeam identifies a team accepted for deletion.
type DeletedTeam struct {
	// Slug is the team slug.
	Slug string `json:"slug"`
}

// CreateTeamOperation creates a team with bounded resend.
type CreateTeamOperation struct{ client *Client }

// GetTeamOperation retrieves one team.
type GetTeamOperation struct{ client *Client }

// ListTeamsOperation lists teams.
type ListTeamsOperation struct{ client *Client }

// UpdateTeamOperation replaces a team's slug and name.
type UpdateTeamOperation struct{ client *Client }

// DeleteTeamOperation schedules a team for deletion.
type DeleteTeamOperation struct{ client *Client }

// CreateTeam binds team creation to this client.
func (client *Client) CreateTeam() CreateTeamOperation { return CreateTeamOperation{client} }

// GetTeam binds team retrieval to this client.
func (client *Client) GetTeam() GetTeamOperation { return GetTeamOperation{client} }

// ListTeams binds team listing to this client.
func (client *Client) ListTeams() ListTeamsOperation { return ListTeamsOperation{client} }

// UpdateTeam binds team updates to this client.
func (client *Client) UpdateTeam() UpdateTeamOperation { return UpdateTeamOperation{client} }

// DeleteTeam binds team deletion to this client.
func (client *Client) DeleteTeam() DeleteTeamOperation { return DeleteTeamOperation{client} }

// Definition returns the generated createTeam contract.
func (CreateTeamOperation) Definition() sdkgo.MutationDefinition { return CreateTeamDefinition }

// Definition returns the generated getTeam contract.
func (GetTeamOperation) Definition() sdkgo.QueryDefinition { return GetTeamDefinition }

// Definition returns the generated listTeams contract.
func (ListTeamsOperation) Definition() sdkgo.QueryDefinition { return ListTeamsDefinition }

// Definition returns the generated updateTeam contract.
func (UpdateTeamOperation) Definition() sdkgo.MutationDefinition { return UpdateTeamDefinition }

// Definition returns the generated deleteTeam contract.
func (DeleteTeamOperation) Definition() sdkgo.MutationDefinition { return DeleteTeamDefinition }

// IdempotencyKey correlates possible create sends.
func (CreateTeamOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTeamInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute updates.
func (UpdateTeamOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTeamInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates deletion attempts.
func (DeleteTeamOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTeamInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a team with an explicit organization-local slug.
func (operation CreateTeamOperation) Invoke(call sdkgo.Call, input CreateTeamInput) sdkgo.MutationAttempt[Team] {
	if !validSentrySegments(input.Organization, input.Slug, input.Name) {
		return mutationFailure[Team]("createTeam", CreateTeamBranchDefect, sdkgo.FailureValidation, "organization, slug, and name are required")
	}
	body, err := json.Marshal(struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}{input.Slug, input.Name})
	if err != nil {
		return mutationFailure[Team]("createTeam", CreateTeamBranchDefect, sdkgo.FailureValidation, "team cannot be encoded")
	}
	request := sentryLimitedCreate[Team]{client: operation.client, call: call, operation: "createTeam", path: teamListPath(input.Organization), body: body, timeout: CreateTeamDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateTeamBranchCreated, rejected: CreateTeamBranchProviderRejected, defect: CreateTeamBranchDefect, decode: func(body []byte) (Team, error) { return decodeTeamSlug(body, input.Slug) }, readBack: func(call sdkgo.Call) (Team, readBackState) { return operation.readBackTeam(call, input) }}
	return request.invoke()
}

// Invoke retrieves one team.
func (operation GetTeamOperation) Invoke(call sdkgo.Call, input GetTeamInput) sdkgo.QueryAttempt[Team] {
	if !validSentrySegments(input.Organization, input.Slug) {
		return queryFailure[Team]("getTeam", GetTeamBranchDefect, sdkgo.FailureValidation, "organization and slug are required")
	}
	return runQuery(operation.client, call, "getTeam", teamPath(input.Organization, input.Slug), GetTeamBranchFound, GetTeamBranchNotFound, GetTeamBranchProviderRejected, GetTeamBranchInvalidResponse, GetTeamBranchDefect, func(body []byte) (Team, error) { return decodeTeamSlug(body, input.Slug) })
}

// Invoke lists one organization page of teams.
func (operation ListTeamsOperation) Invoke(call sdkgo.Call, input ListTeamsInput) sdkgo.QueryAttempt[TeamPage] {
	if !validSentrySegments(input.Organization) {
		return queryFailure[TeamPage]("listTeams", ListTeamsBranchDefect, sdkgo.FailureValidation, "organization is required")
	}
	path := teamListPath(input.Organization)
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
	return runQuery(operation.client, call, "listTeams", path, ListTeamsBranchListed, "", ListTeamsBranchProviderRejected, ListTeamsBranchInvalidResponse, ListTeamsBranchDefect, decodeTeamPage)
}

// Invoke replaces one team's slug and name.
func (operation UpdateTeamOperation) Invoke(call sdkgo.Call, input UpdateTeamInput) sdkgo.MutationAttempt[Team] {
	if !validSentrySegments(input.Organization, input.Slug, input.NewSlug, input.Name) {
		return mutationFailure[Team]("updateTeam", UpdateTeamBranchDefect, sdkgo.FailureValidation, "organization, current slug, new slug, and name are required")
	}
	body, err := json.Marshal(struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}{input.NewSlug, input.Name})
	if err != nil {
		return mutationFailure[Team]("updateTeam", UpdateTeamBranchDefect, sdkgo.FailureValidation, "team update cannot be encoded")
	}
	return runMutation(operation.client, call, "updateTeam", http.MethodPut, teamPath(input.Organization, input.Slug), body, "", "", UpdateTeamBranchUpdated, UpdateTeamBranchNotFound, UpdateTeamBranchProviderRejected, UpdateTeamBranchInvalidResponse, UpdateTeamBranchDefect, func(body []byte) (Team, error) { return decodeTeamSlug(body, input.NewSlug) })
}

// Invoke schedules one team for deletion.
func (operation DeleteTeamOperation) Invoke(call sdkgo.Call, input DeleteTeamInput) sdkgo.MutationAttempt[DeletedTeam] {
	if !validSentrySegments(input.Organization, input.Slug) {
		return mutationFailure[DeletedTeam]("deleteTeam", DeleteTeamBranchDefect, sdkgo.FailureValidation, "organization and slug are required")
	}
	return runMutation(operation.client, call, "deleteTeam", http.MethodDelete, teamPath(input.Organization, input.Slug), nil, "", "", DeleteTeamBranchDeleted, DeleteTeamBranchNotFound, DeleteTeamBranchProviderRejected, DeleteTeamBranchInvalidResponse, DeleteTeamBranchDefect, func(body []byte) (DeletedTeam, error) {
		if len(body) != 0 {
			return DeletedTeam{}, fmt.Errorf("unexpected team deletion content")
		}
		return DeletedTeam{Slug: input.Slug}, nil
	})
}

func (operation CreateTeamOperation) readBackTeam(call sdkgo.Call, input CreateTeamInput) (Team, readBackState) {
	body, state := operation.client.readBackResource(call, teamPath(input.Organization, input.Slug))
	if state != readBackMatched {
		return Team{}, state
	}
	team, err := decodeTeamSlug(body, input.Slug)
	if err != nil {
		return Team{}, readBackRetry
	}
	if team.Name != input.Name {
		return Team{}, readBackConflicted
	}
	return team, readBackMatched
}
func decodeTeamSlug(body []byte, slug string) (Team, error) {
	team, err := decodeTeam(body)
	if err == nil && team.Slug != slug {
		return Team{}, fmt.Errorf("Sentry returned a different team slug")
	}
	return team, err
}
func decodeTeam(body []byte) (Team, error) {
	var team Team
	if err := json.Unmarshal(body, &team); err != nil {
		return Team{}, err
	}
	if strings.TrimSpace(team.ID) == "" || strings.TrimSpace(team.Slug) == "" || strings.TrimSpace(team.Name) == "" {
		return Team{}, fmt.Errorf("team identity is missing")
	}
	return team, nil
}
func decodeTeamPage(body []byte) (TeamPage, error) {
	var teams []Team
	if err := json.Unmarshal(body, &teams); err != nil {
		return TeamPage{}, err
	}
	for _, team := range teams {
		if strings.TrimSpace(team.ID) == "" || strings.TrimSpace(team.Slug) == "" || strings.TrimSpace(team.Name) == "" {
			return TeamPage{}, fmt.Errorf("team page contains an invalid team")
		}
	}
	return TeamPage{Teams: teams}, nil
}
func teamListPath(organization string) string {
	return "/organizations/" + url.PathEscape(organization) + "/teams/"
}
func teamPath(organization, slug string) string {
	return "/teams/" + url.PathEscape(organization) + "/" + url.PathEscape(slug) + "/"
}
