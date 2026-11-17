// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Person contains a Basecamp person identity and visible profile fields.
type Person struct {
	// ID is the numeric person ID.
	ID string `json:"id"`
	// Name is the person's display name.
	Name string `json:"name"`
	// EmailAddress may be redacted by Basecamp for non-admin callers.
	EmailAddress string `json:"emailAddress,omitempty"`
}

// PeoplePage contains one provider page of people.
type PeoplePage struct {
	// People are visible to the authorized identity.
	People []Person `json:"people"`
}

// ListPeopleInput selects one account's people.
type ListPeopleInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
}

// ListProjectPeopleInput selects one project's active people.
type ListProjectPeopleInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// ProjectID comes from listProjects.
	ProjectID string `json:"projectId"`
}

// GetPersonInput identifies one person.
type GetPersonInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// PersonID comes from listPeople or project membership.
	PersonID string `json:"personId"`
}

// ProjectPersonAccessInput identifies an existing person and project.
type ProjectPersonAccessInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// ProjectID comes from listProjects.
	ProjectID string `json:"projectId"`
	// PersonID comes from listPeople.
	PersonID string `json:"personId"`
}

// ProjectPersonAccessResult identifies the requested membership state.
type ProjectPersonAccessResult struct {
	// ProjectID is the selected project.
	ProjectID string `json:"projectId"`
	// PersonID is the selected person.
	PersonID string `json:"personId"`
	// Access is granted or revoked.
	Access string `json:"access"`
}

// EnrollPersonInput invites or re-enrolls a person by email.
type EnrollPersonInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// Name is the person's non-blank display name.
	Name string `json:"name"`
	// EmailAddress is a deliverable email address; it is not a credential.
	EmailAddress string `json:"emailAddress"`
	// ProjectIDs optionally grant visible projects during enrollment.
	ProjectIDs []string `json:"projectIds,omitempty"`
	// Notify controls invitation email; nil uses Basecamp's true default.
	Notify *bool `json:"notify,omitempty"`
}

// EnrollmentResult identifies the enrolled person and provider disposition.
type EnrollmentResult struct {
	// Person is the enrolled or existing account person.
	Person Person `json:"person"`
	// Status is created or existing.
	Status string `json:"status"`
}

// ListPeopleOperation reads visible account people.
type ListPeopleOperation struct{ client *Client }

// ListPeople binds the account people Query.
func (client *Client) ListPeople() ListPeopleOperation { return ListPeopleOperation{client} }

// Definition returns the generated Query contract.
func (ListPeopleOperation) Definition() sdkgo.QueryDefinition { return ListPeopleDefinition }

// Invoke reads one provider page of visible account people.
func (operation ListPeopleOperation) Invoke(call sdkgo.Call, input ListPeopleInput) sdkgo.QueryAttempt[PeoplePage] {
	if !validResourceID(input.AccountID) {
		return resourceBranch[PeoplePage]("listPeople", ListPeopleBranchDefect, sdkgo.FailureValidation, "account ID is invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/people.json")
	return resourceQueryAttempt(response, "listPeople", ListPeopleBranchListed, "", ListPeopleBranchProviderRejected, ListPeopleBranchInvalidResponse, ListPeopleBranchDefect, decodePeople)
}

// ListProjectPeopleOperation reads a project's active roster.
type ListProjectPeopleOperation struct{ client *Client }

// ListProjectPeople binds the project roster Query.
func (client *Client) ListProjectPeople() ListProjectPeopleOperation {
	return ListProjectPeopleOperation{client}
}

// Definition returns the generated Query contract.
func (ListProjectPeopleOperation) Definition() sdkgo.QueryDefinition {
	return ListProjectPeopleDefinition
}

// Invoke reads one provider page of active project members.
func (operation ListProjectPeopleOperation) Invoke(call sdkgo.Call, input ListProjectPeopleInput) sdkgo.QueryAttempt[PeoplePage] {
	if !validResourceID(input.AccountID) || !validResourceID(input.ProjectID) {
		return resourceBranch[PeoplePage]("listProjectPeople", ListProjectPeopleBranchDefect, sdkgo.FailureValidation, "project IDs are invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/projects/"+input.ProjectID+"/people.json")
	return resourceQueryAttempt(response, "listProjectPeople", ListProjectPeopleBranchListed, "", ListProjectPeopleBranchProviderRejected, ListProjectPeopleBranchInvalidResponse, ListProjectPeopleBranchDefect, decodePeople)
}

// GetPersonOperation reads one person profile.
type GetPersonOperation struct{ client *Client }

// GetPerson binds the person Query.
func (client *Client) GetPerson() GetPersonOperation { return GetPersonOperation{client} }

// Definition returns the generated Query contract.
func (GetPersonOperation) Definition() sdkgo.QueryDefinition { return GetPersonDefinition }

// Invoke reads the selected person.
func (operation GetPersonOperation) Invoke(call sdkgo.Call, input GetPersonInput) sdkgo.QueryAttempt[Person] {
	if !validResourceID(input.AccountID) || !validResourceID(input.PersonID) {
		return resourceBranch[Person]("getPerson", GetPersonBranchDefect, sdkgo.FailureValidation, "person IDs are invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/people/"+input.PersonID+".json")
	return resourceQueryAttempt(response, "getPerson", GetPersonBranchFound, GetPersonBranchNotFound, GetPersonBranchProviderRejected, GetPersonBranchInvalidResponse, GetPersonBranchDefect, func(body []byte) (Person, error) { return decodePerson(body, input.PersonID) })
}

// GrantProjectAccessOperation grants one existing person project access.
type GrantProjectAccessOperation struct{ client *Client }

// GrantProjectAccess binds the membership grant Mutation.
func (client *Client) GrantProjectAccess() GrantProjectAccessOperation {
	return GrantProjectAccessOperation{client}
}

// Definition returns the generated Mutation contract.
func (GrantProjectAccessOperation) Definition() sdkgo.MutationDefinition {
	return GrantProjectAccessDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (GrantProjectAccessOperation) IdempotencyKey(callID sdkgo.CallID, _ ProjectPersonAccessInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke grants project access to the selected person.
func (operation GrantProjectAccessOperation) Invoke(call sdkgo.Call, input ProjectPersonAccessInput) sdkgo.MutationAttempt[ProjectPersonAccessResult] {
	return operation.client.changeProjectAccess(call, input, "grantProjectAccess", "grant", "granted", GrantProjectAccessBranchGranted, GrantProjectAccessBranchNotFound, GrantProjectAccessBranchProviderRejected, GrantProjectAccessBranchDefect)
}

// RevokeProjectAccessOperation revokes one person's project access.
type RevokeProjectAccessOperation struct{ client *Client }

// RevokeProjectAccess binds the membership revoke Mutation.
func (client *Client) RevokeProjectAccess() RevokeProjectAccessOperation {
	return RevokeProjectAccessOperation{client}
}

// Definition returns the generated Mutation contract.
func (RevokeProjectAccessOperation) Definition() sdkgo.MutationDefinition {
	return RevokeProjectAccessDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (RevokeProjectAccessOperation) IdempotencyKey(callID sdkgo.CallID, _ ProjectPersonAccessInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke revokes project access from the selected person.
func (operation RevokeProjectAccessOperation) Invoke(call sdkgo.Call, input ProjectPersonAccessInput) sdkgo.MutationAttempt[ProjectPersonAccessResult] {
	return operation.client.changeProjectAccess(call, input, "revokeProjectAccess", "revoke", "revoked", RevokeProjectAccessBranchRevoked, RevokeProjectAccessBranchNotFound, RevokeProjectAccessBranchProviderRejected, RevokeProjectAccessBranchDefect)
}

func (client *Client) changeProjectAccess(call sdkgo.Call, input ProjectPersonAccessInput, operation, field, access string, success, notFound, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[ProjectPersonAccessResult] {
	if !validResourceID(input.AccountID) || !validResourceID(input.ProjectID) || !validResourceID(input.PersonID) {
		return projectMutationBranch[ProjectPersonAccessResult](operation, defect, sdkgo.FailureValidation, "project or person IDs are invalid")
	}
	personID, _ := strconv.ParseInt(input.PersonID, 10, 64)
	response := client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/projects/"+input.ProjectID+"/people/users.json", map[string][]int64{field: {personID}})
	if attempt, done := projectMutationResponse[ProjectPersonAccessResult](response, operation, http.StatusOK, notFound, rejected, defect, rejected); done {
		return attempt
	}
	return sdkgo.NewMutationBranch(success, ProjectPersonAccessResult{ProjectID: input.ProjectID, PersonID: input.PersonID, Access: access}, nil, sdkgo.Receipt{})
}

// EnrollPersonOperation enrolls or invites one account person.
type EnrollPersonOperation struct{ client *Client }

// EnrollPerson binds the account enrollment Mutation.
func (client *Client) EnrollPerson() EnrollPersonOperation { return EnrollPersonOperation{client} }

// Definition returns the generated Mutation contract.
func (EnrollPersonOperation) Definition() sdkgo.MutationDefinition { return EnrollPersonDefinition }

// IdempotencyKey records the Dex call identity; Basecamp does not deduplicate it.
func (EnrollPersonOperation) IdempotencyKey(callID sdkgo.CallID, _ EnrollPersonInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke enrolls a person, bounding possibly repeated invitation emails.
func (operation EnrollPersonOperation) Invoke(call sdkgo.Call, input EnrollPersonInput) sdkgo.MutationAttempt[EnrollmentResult] {
	address, addressErr := mail.ParseAddress(input.EmailAddress)
	if !validResourceID(input.AccountID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 || addressErr != nil || address.Address != input.EmailAddress {
		return projectMutationBranch[EnrollmentResult]("enrollPerson", EnrollPersonBranchDefect, sdkgo.FailureValidation, "enrollment input is invalid")
	}
	projectIDs := make([]int64, 0, len(input.ProjectIDs))
	for _, value := range input.ProjectIDs {
		if !validResourceID(value) {
			return projectMutationBranch[EnrollmentResult]("enrollPerson", EnrollPersonBranchDefect, sdkgo.FailureValidation, "project ID is invalid")
		}
		projectID, _ := strconv.ParseInt(value, 10, 64)
		projectIDs = append(projectIDs, projectID)
	}
	fence, err := singledispatch.NewFence(call, EnrollPersonDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return projectMutationBranch[EnrollmentResult]("enrollPerson", EnrollPersonBranchDefect, sdkgo.FailureLocalDefect, "enrollment checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return projectMutationUncertain[EnrollmentResult]("enrollPerson", "enrollment may have completed without confirmation")
	}
	requestBody := struct {
		People []struct {
			Name         string `json:"name"`
			EmailAddress string `json:"email_address"`
		} `json:"people"`
		ProjectIDs []int64 `json:"project_ids,omitempty"`
		Notify     *bool   `json:"notify,omitempty"`
	}{ProjectIDs: projectIDs, Notify: input.Notify}
	requestBody.People = []struct {
		Name         string `json:"name"`
		EmailAddress string `json:"email_address"`
	}{{Name: input.Name, EmailAddress: input.EmailAddress}}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.accountMutation(call, requestContext, http.MethodPost, input.AccountID, "/account/enrollments/people.json", requestBody)
		return nil
	})
	if err != nil {
		return projectMutationRetry[EnrollmentResult]("enrollPerson", sdkgo.FailureAvailability, "enrollment dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return projectMutationRetry[EnrollmentResult]("enrollPerson", sdkgo.FailureAvailability, "enrollment resend checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return projectMutationRetry[EnrollmentResult]("enrollPerson", kind, "Basecamp temporarily rejected enrollment", response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[EnrollmentResult]("enrollPerson", "an earlier enrollment may have completed")
		}
		return projectMutationBranch[EnrollmentResult]("enrollPerson", EnrollPersonBranchDefect, sdkgo.FailureAuthentication, "Basecamp credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return projectMutationRetry[EnrollmentResult]("enrollPerson", sdkgo.FailureTransport, "enrollment outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[EnrollmentResult]("enrollPerson", "an earlier enrollment may have completed")
		}
		return projectMutationBranch[EnrollmentResult]("enrollPerson", EnrollPersonBranchProviderRejected, sdkgo.FailureProviderRejection, "Basecamp rejected enrollment")
	}
	result, err := decodeEnrollment(response.body, input.Name)
	if err != nil {
		return projectMutationRetry[EnrollmentResult]("enrollPerson", sdkgo.FailureProtocol, "Basecamp returned invalid enrollment JSON", 0)
	}
	return sdkgo.NewMutationBranch(EnrollPersonBranchEnrolled, result, nil, sdkgo.Receipt{})
}

func decodePeople(body []byte) (PeoplePage, error) {
	var records []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		EmailAddress string `json:"email_address"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil {
		return PeoplePage{}, fmt.Errorf("people are invalid")
	}
	page := PeoplePage{People: make([]Person, 0, len(records))}
	for _, record := range records {
		if record.ID <= 0 || strings.TrimSpace(record.Name) == "" {
			return PeoplePage{}, fmt.Errorf("person is invalid")
		}
		page.People = append(page.People, Person{ID: strconv.FormatInt(record.ID, 10), Name: record.Name, EmailAddress: record.EmailAddress})
	}
	return page, nil
}

func decodePerson(body []byte, expectedID string) (Person, error) {
	var record struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		EmailAddress string `json:"email_address"`
	}
	if err := json.Unmarshal(body, &record); err != nil || record.ID <= 0 || (expectedID != "" && strconv.FormatInt(record.ID, 10) != expectedID) || strings.TrimSpace(record.Name) == "" {
		return Person{}, fmt.Errorf("person is invalid")
	}
	return Person{ID: strconv.FormatInt(record.ID, 10), Name: record.Name, EmailAddress: record.EmailAddress}, nil
}

func decodeEnrollment(body []byte, expectedName string) (EnrollmentResult, error) {
	var records []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		EmailAddress string `json:"email_address"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(body, &records); err != nil || len(records) != 1 || records[0].ID <= 0 || records[0].Name != expectedName || (records[0].Status != "created" && records[0].Status != "existing") {
		return EnrollmentResult{}, fmt.Errorf("enrollment is invalid")
	}
	return EnrollmentResult{Person: Person{ID: strconv.FormatInt(records[0].ID, 10), Name: records[0].Name, EmailAddress: records[0].EmailAddress}, Status: records[0].Status}, nil
}
