// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package huaweicloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAccessibleProjectsInput has no user-supplied query parameters.
type ListAccessibleProjectsInput struct{}

// AccessibleProject contains non-secret IAM project metadata.
type AccessibleProject struct {
	// ID is the provider's stable project identifier.
	ID string `json:"id"`
	// Name is the project name, which can be empty in Huawei Cloud responses.
	Name string `json:"name"`
	// Enabled reports whether the project is enabled.
	Enabled bool `json:"enabled"`
	// DomainID is the owning account identifier.
	DomainID string `json:"domainId"`
}

// ListAccessibleProjectsOutput contains all projects returned for the user.
type ListAccessibleProjectsOutput struct {
	// Projects is the accessible project set; an empty set is valid.
	Projects []AccessibleProject `json:"projects"`
}

// ListAccessibleProjectsOperation obtains a domain-scoped token, then lists projects.
type ListAccessibleProjectsOperation struct{ client *Client }

// Definition returns the generated project-list contract.
func (ListAccessibleProjectsOperation) Definition() sdkgo.QueryDefinition {
	return ListAccessibleProjectsDefinition
}

// Invoke performs only read-only IAM operations and keeps the token and password out of durable state.
func (operation ListAccessibleProjectsOperation) Invoke(call sdkgo.Call, _ ListAccessibleProjectsInput) sdkgo.QueryAttempt[ListAccessibleProjectsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AccountName.Reveal()) || !isValidCredential(credentials.IAMUsername.Reveal()) || !isValidCredential(credentials.IAMPassword.Reveal()) {
		return projectBranch(ListAccessibleProjectsBranchDefect, sdkgo.FailureAuthentication, "IAM credentials are unavailable or invalid")
	}
	tokenRequestBody := struct {
		Auth struct {
			Identity struct {
				Methods  []string `json:"methods"`
				Password struct {
					User struct {
						Domain struct {
							Name string `json:"name"`
						} `json:"domain"`
						Name     string `json:"name"`
						Password string `json:"password"`
					} `json:"user"`
				} `json:"password"`
			} `json:"identity"`
			Scope struct {
				Domain struct {
					Name string `json:"name"`
				} `json:"domain"`
			} `json:"scope"`
		} `json:"auth"`
	}{}
	tokenRequestBody.Auth.Identity.Methods = []string{"password"}
	tokenRequestBody.Auth.Identity.Password.User.Domain.Name = credentials.AccountName.Reveal()
	tokenRequestBody.Auth.Identity.Password.User.Name = credentials.IAMUsername.Reveal()
	tokenRequestBody.Auth.Identity.Password.User.Password = credentials.IAMPassword.Reveal()
	tokenRequestBody.Auth.Scope.Domain.Name = credentials.AccountName.Reveal()
	encoded, err := json.Marshal(tokenRequestBody)
	if err != nil {
		return projectBranch(ListAccessibleProjectsBranchDefect, sdkgo.FailureValidation, "IAM token request could not be encoded")
	}
	tokenResponse, tokenErr := operation.client.request(call, http.MethodPost, "/v3/auth/tokens", encoded, "")
	if tokenErr != nil {
		return *tokenErr
	}
	if tokenResponse.status != http.StatusCreated {
		return classifyIAMStatus(tokenResponse.status, "token")
	}
	token := tokenResponse.token
	if !isValidToken(token) {
		return projectBranch(ListAccessibleProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "IAM returned an invalid token header")
	}
	projectsResponse, projectsErr := operation.client.request(call, http.MethodGet, "/v3/auth/projects", nil, token)
	if projectsErr != nil {
		return *projectsErr
	}
	if projectsResponse.status != http.StatusOK {
		return classifyIAMStatus(projectsResponse.status, "projects")
	}
	var wire struct {
		Projects *[]struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Enabled  *bool  `json:"enabled"`
			DomainID string `json:"domain_id"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(projectsResponse.body, &wire); err != nil || wire.Projects == nil {
		return projectBranch(ListAccessibleProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "IAM returned invalid project data")
	}
	output := ListAccessibleProjectsOutput{Projects: make([]AccessibleProject, 0, len(*wire.Projects))}
	for _, project := range *wire.Projects {
		if project.ID == "" || project.Enabled == nil || project.DomainID == "" {
			return projectBranch(ListAccessibleProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "IAM returned invalid project metadata")
		}
		output.Projects = append(output.Projects, AccessibleProject{ID: project.ID, Name: project.Name, Enabled: *project.Enabled, DomainID: project.DomainID})
	}
	return sdkgo.NewQueryBranch(ListAccessibleProjectsBranchFound, output, nil, sdkgo.Receipt{})
}

type iamResponse struct {
	status int
	token  string
	body   []byte
}

func (client *Client) request(call sdkgo.Call, method, path string, body []byte, token string) (iamResponse, *sdkgo.QueryAttempt[ListAccessibleProjectsOutput]) {
	request, err := http.NewRequestWithContext(call.Context, method, iamEndpoint+path, bytes.NewReader(body))
	if err != nil {
		attempt := projectBranch(ListAccessibleProjectsBranchDefect, sdkgo.FailureValidation, "IAM request is invalid")
		return iamResponse{}, &attempt
	}
	request.Header.Set("Content-Type", "application/json;charset=utf8")
	request.Header.Set("Accept", "application/json")
	if token != "" {
		request.Header.Set("X-Auth-Token", token)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		attempt := projectRetry(sdkgo.FailureAvailability, "Huawei Cloud IAM request failed", 0)
		return iamResponse{}, &attempt
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		attempt := projectRetry(sdkgo.FailureRateLimit, "Huawei Cloud IAM rate limited the request", retryAfter)
		return iamResponse{}, &attempt
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		attempt := projectRetry(sdkgo.FailureAvailability, "Huawei Cloud IAM is temporarily unavailable", retryAfter)
		return iamResponse{}, &attempt
	}
	limitedBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := projectBranch(ListAccessibleProjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Huawei Cloud IAM response exceeded the configured limit")
		return iamResponse{}, &attempt
	}
	if err != nil {
		attempt := projectRetry(sdkgo.FailureAvailability, "Huawei Cloud IAM response was interrupted", 0)
		return iamResponse{}, &attempt
	}
	return iamResponse{status: response.StatusCode, token: response.Header.Get("X-Subject-Token"), body: limitedBody}, nil
}
func isValidToken(token string) bool {
	if token == "" || len(token) >= 32768 {
		return false
	}
	for _, character := range token {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyIAMStatus(status int, phase string) sdkgo.QueryAttempt[ListAccessibleProjectsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return projectBranch(ListAccessibleProjectsBranchProviderRejected, kind, "Huawei Cloud IAM rejected the "+phase+" request with HTTP "+strconv.Itoa(status))
}
func projectBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListAccessibleProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "huaweicloud", Operation: "listAccessibleProjects", Message: message}
	return sdkgo.NewQueryBranch(branch, ListAccessibleProjectsOutput{}, &failure, sdkgo.Receipt{})
}
func projectRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListAccessibleProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "huaweicloud", Operation: "listAccessibleProjects", Message: message}
	return sdkgo.NewQueryRetry[ListAccessibleProjectsOutput](failure, delay)
}
