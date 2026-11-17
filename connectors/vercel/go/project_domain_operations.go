// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ProjectDomain is a domain assigned to a Vercel project.
type ProjectDomain struct {
	// Name is the fully qualified domain name.
	Name string `json:"name"`
	// ProjectID identifies the project that owns the assignment.
	ProjectID string `json:"projectId"`
	// GitBranch is the linked branch when present.
	GitBranch string `json:"gitBranch"`
	// Verified reports whether the DNS challenge has passed.
	Verified bool `json:"verified"`
}

// ProjectDomainsPage contains one page of project domain assignments.
type ProjectDomainsPage struct {
	// Domains contains the assigned domains.
	Domains []ProjectDomain `json:"domains"`
	// Pagination carries the next cursor when another page exists.
	Pagination struct {
		Next *int64 `json:"next"`
	} `json:"pagination"`
}

// ListProjectDomainsInput identifies a project and optional team.
type ListProjectDomainsInput struct {
	// ProjectID is the Vercel project ID or name.
	ProjectID string `json:"projectId"`
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// ProjectDomainInput identifies a project domain and optional team.
type ProjectDomainInput struct {
	// ProjectID is the Vercel project ID or name.
	ProjectID string `json:"projectId"`
	// Domain is the fully qualified domain name.
	Domain string `json:"domain"`
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// UpdateProjectDomainInput assigns a domain to one Git branch.
type UpdateProjectDomainInput struct {
	// ProjectID is the Vercel project ID or name.
	ProjectID string `json:"projectId"`
	// Domain is the fully qualified domain name.
	Domain string `json:"domain"`
	// GitBranch is the branch to assign.
	GitBranch string `json:"gitBranch"`
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// RemovedProjectDomain identifies a domain assignment now absent.
type RemovedProjectDomain struct {
	// ProjectID is the project from which the domain was removed.
	ProjectID string `json:"projectId"`
	// Name is the domain removed from the project.
	Name string `json:"name"`
}

// ListProjectDomainsOperation reads a project's domain assignments.
type ListProjectDomainsOperation struct{ client *Client }

// ListProjectDomains binds a domain listing to this client.
func (client *Client) ListProjectDomains() ListProjectDomainsOperation {
	return ListProjectDomainsOperation{client}
}

// Definition returns the generated Query contract.
func (ListProjectDomainsOperation) Definition() sdkgo.QueryDefinition {
	return ListProjectDomainsDefinition
}

// Invoke reads one page of domain assignments.
func (operation ListProjectDomainsOperation) Invoke(call sdkgo.Call, input ListProjectDomainsInput) sdkgo.QueryAttempt[ProjectDomainsPage] {
	if !validProjectIdentifier(input.ProjectID) || !validTeamID(input.TeamID) {
		return projectQueryFailure[ProjectDomainsPage]("listProjectDomains", "defect", sdkgo.FailureValidation)
	}
	return executeQuery(operation.client, call, "listProjectDomains", requestSpec{method: http.MethodGet, path: projectDomainsPath(input.ProjectID), query: teamQuery(input.TeamID)}, "listed", "notFound", "providerRejected", "invalidResponse", "defect", decodeProjectDomainsPage)
}

// GetProjectDomainOperation reads one domain assignment.
type GetProjectDomainOperation struct{ client *Client }

// GetProjectDomain binds a domain lookup to this client.
func (client *Client) GetProjectDomain() GetProjectDomainOperation {
	return GetProjectDomainOperation{client}
}

// Definition returns the generated Query contract.
func (GetProjectDomainOperation) Definition() sdkgo.QueryDefinition {
	return GetProjectDomainDefinition
}

// Invoke reads one project domain.
func (operation GetProjectDomainOperation) Invoke(call sdkgo.Call, input ProjectDomainInput) sdkgo.QueryAttempt[ProjectDomain] {
	if !validProjectDomainInput(input) {
		return projectQueryFailure[ProjectDomain]("getProjectDomain", "defect", sdkgo.FailureValidation)
	}
	return executeQuery(operation.client, call, "getProjectDomain", requestSpec{method: http.MethodGet, path: projectDomainPath(input.ProjectID, input.Domain), query: teamQuery(input.TeamID)}, "found", "notFound", "providerRejected", "invalidResponse", "defect", decodeProjectDomain)
}

// AddProjectDomainOperation assigns a domain with bounded possible sends.
type AddProjectDomainOperation struct{ client *Client }

// AddProjectDomain binds domain assignment to this client.
func (client *Client) AddProjectDomain() AddProjectDomainOperation {
	return AddProjectDomainOperation{client}
}

// Definition returns the generated Mutation contract.
func (AddProjectDomainOperation) Definition() sdkgo.MutationDefinition {
	return AddProjectDomainDefinition
}

// IdempotencyKey identifies the logical domain assignment.
func (AddProjectDomainOperation) IdempotencyKey(callID sdkgo.CallID, _ ProjectDomainInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke assigns a domain and reports uncertainty after the possible-send cap.
func (operation AddProjectDomainOperation) Invoke(call sdkgo.Call, input ProjectDomainInput) sdkgo.MutationAttempt[ProjectDomain] {
	if !validProjectDomainInput(input) {
		return mutationFailure[ProjectDomain]("addProjectDomain", "defect", sdkgo.FailureValidation, "Vercel project domain input is invalid")
	}
	body, err := json.Marshal(map[string]string{"name": input.Domain})
	if err != nil {
		return mutationFailure[ProjectDomain]("addProjectDomain", "defect", sdkgo.FailureLocalDefect, "Vercel domain request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, AddProjectDomainDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationFailure[ProjectDomain]("addProjectDomain", "defect", sdkgo.FailureLocalDefect, "Vercel domain dispatch could not be initialized")
	}
	decision := fence.Decide()
	// A domain name cannot identify the call that assigned it.
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return domainUncertain()
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response *http.Response
	var localFailure *sdkgo.Failure
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		response, localFailure, sendErr = operation.client.sendWithContext(call, requestContext, "addProjectDomain", requestSpec{method: http.MethodPost, path: "/v10/projects/" + url.PathEscape(input.ProjectID) + "/domains", query: teamQuery(input.TeamID), contentType: "application/json", body: body}, true)
		return sendErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return mutationRetry[ProjectDomain]("addProjectDomain", sdkgo.FailureAvailability, 0)
	}
	if localFailure != nil {
		return domainUncertain()
	}
	if response == nil {
		if canResend {
			return mutationRetry[ProjectDomain]("addProjectDomain", sdkgo.FailureTransport, 0)
		}
		return domainUncertain()
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return mutationRetry[ProjectDomain]("addProjectDomain", sdkgo.FailureAvailability, 0)
		}
		kind, delay := retryFailure(response, "addProjectDomain")
		return sdkgo.NewMutationRetry[ProjectDomain](kind, delay)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 || err != nil {
		if canResend {
			return mutationRetry[ProjectDomain]("addProjectDomain", sdkgo.FailureAvailability, 0)
		}
		return domainUncertain()
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return domainUncertain()
		}
		return mutationFailure[ProjectDomain]("addProjectDomain", "providerRejected", sdkgo.FailureProviderRejection, "Vercel rejected domain assignment")
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return domainUncertain()
	}
	domain, err := decodeProjectDomain(responseBody)
	if err != nil || domain.Name != input.Domain {
		return domainUncertain()
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("added"), domain, nil, sdkgo.Receipt{})
}

// UpdateProjectDomainOperation assigns a domain to an absolute Git branch.
type UpdateProjectDomainOperation struct{ client *Client }

// UpdateProjectDomain binds branch assignment to this client.
func (client *Client) UpdateProjectDomain() UpdateProjectDomainOperation {
	return UpdateProjectDomainOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateProjectDomainOperation) Definition() sdkgo.MutationDefinition {
	return UpdateProjectDomainDefinition
}

// IdempotencyKey identifies retries of one branch assignment.
func (UpdateProjectDomainOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProjectDomainInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the domain's linked branch.
func (operation UpdateProjectDomainOperation) Invoke(call sdkgo.Call, input UpdateProjectDomainInput) sdkgo.MutationAttempt[ProjectDomain] {
	if !validProjectDomainInput(ProjectDomainInput{ProjectID: input.ProjectID, Domain: input.Domain, TeamID: input.TeamID}) || input.GitBranch == "" || len(input.GitBranch) > 128 || strings.ContainsAny(input.GitBranch, "?# ") {
		return mutationFailure[ProjectDomain]("updateProjectDomain", "defect", sdkgo.FailureValidation, "Vercel project domain input is invalid")
	}
	body, err := json.Marshal(map[string]string{"gitBranch": input.GitBranch})
	if err != nil {
		return mutationFailure[ProjectDomain]("updateProjectDomain", "defect", sdkgo.FailureLocalDefect, "Vercel domain request could not be encoded")
	}
	return executeDomainMutation(operation.client, call, "updateProjectDomain", requestSpec{method: http.MethodPatch, path: projectDomainPath(input.ProjectID, input.Domain), query: teamQuery(input.TeamID), contentType: "application/json", body: body}, "updated", true, input.GitBranch)
}

// RemoveProjectDomainOperation removes one project assignment.
type RemoveProjectDomainOperation struct{ client *Client }

// RemoveProjectDomain binds removal to this client.
func (client *Client) RemoveProjectDomain() RemoveProjectDomainOperation {
	return RemoveProjectDomainOperation{client}
}

// Definition returns the generated Mutation contract.
func (RemoveProjectDomainOperation) Definition() sdkgo.MutationDefinition {
	return RemoveProjectDomainDefinition
}

// IdempotencyKey identifies retries of one removal.
func (RemoveProjectDomainOperation) IdempotencyKey(callID sdkgo.CallID, _ ProjectDomainInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes the assignment; an absent assignment already meets the target state.
func (operation RemoveProjectDomainOperation) Invoke(call sdkgo.Call, input ProjectDomainInput) sdkgo.MutationAttempt[RemovedProjectDomain] {
	if !validProjectDomainInput(input) {
		return mutationFailure[RemovedProjectDomain]("removeProjectDomain", "defect", sdkgo.FailureValidation, "Vercel project domain input is invalid")
	}
	response, localFailure, err := operation.client.send(call, "removeProjectDomain", requestSpec{method: http.MethodDelete, path: projectDomainPath(input.ProjectID, input.Domain), query: teamQuery(input.TeamID)})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("defect"), RemovedProjectDomain{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return mutationRetry[RemovedProjectDomain]("removeProjectDomain", sdkgo.FailureTransport, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		kind, delay := retryFailure(response, "removeProjectDomain")
		return sdkgo.NewMutationRetry[RemovedProjectDomain](kind, delay)
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return mutationFailure[RemovedProjectDomain]("removeProjectDomain", "providerRejected", sdkgo.FailureProviderRejection, "Vercel rejected domain removal")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("removed"), RemovedProjectDomain{ProjectID: input.ProjectID, Name: input.Domain}, nil, sdkgo.Receipt{})
}

// VerifyProjectDomainOperation checks the domain's DNS challenge.
type VerifyProjectDomainOperation struct{ client *Client }

// VerifyProjectDomain binds verification to this client.
func (client *Client) VerifyProjectDomain() VerifyProjectDomainOperation {
	return VerifyProjectDomainOperation{client}
}

// Definition returns the generated Mutation contract.
func (VerifyProjectDomainOperation) Definition() sdkgo.MutationDefinition {
	return VerifyProjectDomainDefinition
}

// IdempotencyKey identifies retries of one verification.
func (VerifyProjectDomainOperation) IdempotencyKey(callID sdkgo.CallID, _ ProjectDomainInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke verifies a domain after its DNS challenge is satisfied.
func (operation VerifyProjectDomainOperation) Invoke(call sdkgo.Call, input ProjectDomainInput) sdkgo.MutationAttempt[ProjectDomain] {
	if !validProjectDomainInput(input) {
		return mutationFailure[ProjectDomain]("verifyProjectDomain", "defect", sdkgo.FailureValidation, "Vercel project domain input is invalid")
	}
	return executeDomainMutation(operation.client, call, "verifyProjectDomain", requestSpec{method: http.MethodPost, path: projectDomainPath(input.ProjectID, input.Domain) + "/verify", query: teamQuery(input.TeamID)}, "verified", false, "")
}

func executeDomainMutation(client *Client, call sdkgo.Call, operation string, spec requestSpec, happy sdkgo.BranchID, checkBranch bool, gitBranch string) sdkgo.MutationAttempt[ProjectDomain] {
	response, localFailure, err := client.send(call, operation, spec)
	if localFailure != nil {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("defect"), ProjectDomain{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return mutationRetry[ProjectDomain](operation, sdkgo.FailureTransport, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		kind, delay := retryFailure(response, operation)
		return sdkgo.NewMutationRetry[ProjectDomain](kind, delay)
	}
	if response.StatusCode == http.StatusNotFound {
		return mutationFailure[ProjectDomain](operation, "notFound", sdkgo.FailureNotFound, "Vercel did not find the project domain")
	}
	if response.StatusCode != http.StatusOK {
		return mutationFailure[ProjectDomain](operation, "providerRejected", sdkgo.FailureProviderRejection, "Vercel rejected the project domain change")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return mutationFailure[ProjectDomain](operation, "invalidResponse", sdkgo.FailureResponseTooLarge, "Vercel domain response was too large")
	}
	if err != nil {
		return mutationRetry[ProjectDomain](operation, sdkgo.FailureTransport, 0)
	}
	domain, err := decodeProjectDomain(body)
	if err != nil || checkBranch && domain.GitBranch != gitBranch {
		return mutationFailure[ProjectDomain](operation, "invalidResponse", sdkgo.FailureProtocol, "Vercel domain response was malformed")
	}
	return sdkgo.NewMutationBranch(happy, domain, nil, sdkgo.Receipt{})
}

func projectDomainsPath(projectID string) string {
	return "/v9/projects/" + url.PathEscape(projectID) + "/domains"
}
func projectDomainPath(projectID, domain string) string {
	return projectDomainsPath(projectID) + "/" + url.PathEscape(domain)
}
func validProjectDomainInput(input ProjectDomainInput) bool {
	return validProjectIdentifier(input.ProjectID) && validTeamID(input.TeamID) && len(input.Domain) <= 253 && strings.Contains(input.Domain, ".") && !strings.ContainsAny(input.Domain, "/?# ")
}
func decodeProjectDomain(body []byte) (ProjectDomain, error) {
	var domain ProjectDomain
	if err := json.Unmarshal(body, &domain); err != nil {
		return ProjectDomain{}, err
	}
	if domain.Name == "" || domain.ProjectID == "" {
		return ProjectDomain{}, errors.New("missing domain identity")
	}
	return domain, nil
}
func decodeProjectDomainsPage(body []byte) (ProjectDomainsPage, error) {
	var page ProjectDomainsPage
	if err := json.Unmarshal(body, &page); err != nil {
		return ProjectDomainsPage{}, err
	}
	if page.Domains == nil {
		return ProjectDomainsPage{}, errors.New("missing domains")
	}
	for _, domain := range page.Domains {
		if domain.Name == "" || domain.ProjectID == "" {
			return ProjectDomainsPage{}, errors.New("missing domain identity")
		}
	}
	return page, nil
}
func domainUncertain() sdkgo.MutationAttempt[ProjectDomain] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "vercel", Operation: "addProjectDomain", Message: "Vercel domain assignment may have succeeded but could not be confirmed"}
	return sdkgo.NewMutationUncertain(ProjectDomain{}, failure, sdkgo.Receipt{})
}
