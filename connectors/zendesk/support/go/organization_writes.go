// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateOrganizationInput creates an organization with an optional application identifier.
type CreateOrganizationInput struct {
	// Name is the nonempty Zendesk organization name.
	Name string `json:"name"`
	// ExternalID is account-unique; blank derives one from the Dex call ID.
	ExternalID string `json:"externalId"`
	// DomainNames are domains Zendesk uses for automatic organization matching.
	DomainNames []string `json:"domainNames,omitempty"`
}

// CreateOrganizationOutput contains the created or confirmed organization.
type CreateOrganizationOutput struct {
	// Organization is the organization Zendesk returned.
	Organization Organization `json:"organization"`
}

// UpdateOrganizationInput replaces selected organization fields.
type UpdateOrganizationInput struct {
	// OrganizationID is the positive Zendesk organization ID.
	OrganizationID int64 `json:"organizationId"`
	// Name replaces the organization name when supplied.
	Name *string `json:"name,omitempty"`
	// DomainNames replaces domain matching rules when supplied.
	DomainNames []string `json:"domainNames,omitempty"`
	// Tags replaces organization tags when supplied.
	Tags []string `json:"tags,omitempty"`
	// GroupID replaces the default ticket group when supplied.
	GroupID *int64 `json:"groupId,omitempty"`
	// SharedTickets replaces the member ticket-sharing setting when supplied.
	SharedTickets *bool `json:"sharedTickets,omitempty"`
	// SharedComments replaces the member comment-sharing setting when supplied.
	SharedComments *bool `json:"sharedComments,omitempty"`
}

// UpdateOrganizationOutput contains the updated organization.
type UpdateOrganizationOutput struct {
	// Organization is the organization Zendesk returned after the update.
	Organization Organization `json:"organization"`
}

// DeleteOrganizationInput identifies an organization for deletion.
type DeleteOrganizationInput struct {
	// OrganizationID is the positive Zendesk organization ID.
	OrganizationID int64 `json:"organizationId"`
}

// DeleteOrganizationOutput identifies the deleted organization.
type DeleteOrganizationOutput struct {
	// OrganizationID is the deleted organization's ID.
	OrganizationID int64 `json:"organizationId"`
}

// CreateOrganizationOperation creates an organization under a bounded send cap.
type CreateOrganizationOperation struct{ client *Client }

// UpdateOrganizationOperation applies repeatable organization replacements.
type UpdateOrganizationOperation struct{ client *Client }

// DeleteOrganizationOperation deletes one organization.
type DeleteOrganizationOperation struct{ client *Client }

// CreateOrganization binds the organization create Mutation to this client.
func (client *Client) CreateOrganization() CreateOrganizationOperation {
	return CreateOrganizationOperation{client}
}

// UpdateOrganization binds the organization update Mutation to this client.
func (client *Client) UpdateOrganization() UpdateOrganizationOperation {
	return UpdateOrganizationOperation{client}
}

// DeleteOrganization binds the organization delete Mutation to this client.
func (client *Client) DeleteOrganization() DeleteOrganizationOperation {
	return DeleteOrganizationOperation{client}
}

// Definition returns the generated organization create Mutation contract.
func (CreateOrganizationOperation) Definition() sdkgo.MutationDefinition {
	return CreateOrganizationDefinition
}

// Definition returns the generated organization update Mutation contract.
func (UpdateOrganizationOperation) Definition() sdkgo.MutationDefinition {
	return UpdateOrganizationDefinition
}

// Definition returns the generated organization delete Mutation contract.
func (DeleteOrganizationOperation) Definition() sdkgo.MutationDefinition {
	return DeleteOrganizationDefinition
}

// IdempotencyKey correlates retries; Zendesk does not deduplicate organization creates with this key.
func (CreateOrganizationOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOrganizationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates retries of one organization update.
func (UpdateOrganizationOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOrganizationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates retries of one organization deletion.
func (DeleteOrganizationOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteOrganizationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an organization and reads back its unique external ID after an unknown result.
func (operation CreateOrganizationOperation) Invoke(call sdkgo.Call, input CreateOrganizationInput) sdkgo.MutationAttempt[CreateOrganizationOutput] {
	const operationName = "createOrganization"
	if input.ExternalID == "" {
		input.ExternalID = "dex-" + string(call.ID)
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.ExternalID) == "" || len(input.ExternalID) > 255 || strings.ContainsAny(input.ExternalID, "\r\n\t") || !validOrganizationDomains(input.DomainNames) {
		return sdkgo.NewMutationBranch(CreateOrganizationBranchDefect, CreateOrganizationOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "name, valid unique externalId, and valid domainNames are required"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateOrganizationDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateOrganizationBranchDefect, CreateOrganizationOutput{}, zendeskFailurePointer(sdkgo.FailureLocalDefect, operationName, "organization dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		if decision.Kind == singledispatch.SendAllowed {
			_ = fence.RecordNotSent()
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreateOrganizationOutput{}, *failure, operation.client.receipt(call, zendeskResponse{}, 0))
		}
		return sdkgo.NewMutationBranch(CreateOrganizationBranchDefect, CreateOrganizationOutput{}, failure, sdkgo.Receipt{})
	}
	if decision.Marker.ProviderID != "" && decision.Marker.CallID == call.ID {
		read := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/organizations/" + url.PathEscape(decision.Marker.ProviderID)})
		if read.outcome == exchangeSucceeded {
			organization, decodeErr := decodeOrganizationEnvelope(read.response.body)
			if decodeErr == nil && matchesCreatedOrganization(organization, input) {
				return sdkgo.NewMutationBranch(CreateOrganizationBranchCreated, CreateOrganizationOutput{Organization: organization}, nil, operation.client.receipt(call, read.response, organization.ID))
			}
		}
		return sdkgo.NewMutationRetry[CreateOrganizationOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "created organization read-back is unavailable"), 0)
	}
	if decision.MaximumPriorSends > 0 {
		query := url.Values{"external_id": {input.ExternalID}}
		read := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/organizations/search", query: query})
		if read.outcome == exchangeSucceeded {
			var envelope struct {
				Organizations []zendeskOrganizationRecord `json:"organizations"`
			}
			if err := json.Unmarshal(read.response.body, &envelope); err != nil || envelope.Organizations == nil {
				return sdkgo.NewMutationRetry[CreateOrganizationOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "organization read-back is invalid"), 0)
			}
			for _, record := range envelope.Organizations {
				organization, decodeErr := decodeOrganizationRecord(record)
				if decodeErr != nil {
					return sdkgo.NewMutationRetry[CreateOrganizationOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "organization read-back is invalid"), 0)
				}
				if matchesCreatedOrganization(organization, input) {
					return sdkgo.NewMutationBranch(CreateOrganizationBranchCreated, CreateOrganizationOutput{Organization: organization}, nil, operation.client.receipt(call, read.response, organization.ID))
				}
				if strings.EqualFold(organization.ExternalID, input.ExternalID) {
					return sdkgo.NewMutationUncertain(CreateOrganizationOutput{}, zendeskFailure(sdkgo.FailureConflict, operationName, "external ID belongs to an unconfirmed organization"), operation.client.receipt(call, read.response, organization.ID))
				}
			}
		} else {
			return sdkgo.NewMutationRetry[CreateOrganizationOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "organization read-back is unavailable"), read.retryAfter)
		}
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(CreateOrganizationOutput{}, zendeskFailure(sdkgo.FailureTransport, operationName, "an earlier organization create may have succeeded"), operation.client.receipt(call, zendeskResponse{}, 0))
	}
	var result zendeskExchange
	err = fence.Dispatch(func(ctx context.Context) error {
		fields := map[string]any{"name": input.Name, "external_id": input.ExternalID}
		if input.DomainNames != nil {
			fields["domain_names"] = input.DomainNames
		}
		result = operation.client.exchangeWithContext(ctx, credentials, operationName, zendeskRequest{method: http.MethodPost, path: "/organizations", payload: map[string]any{"organization": fields}})
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CreateOrganizationOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "organization dispatch checkpoint is unavailable"), 0)
	}
	receipt := operation.client.receipt(call, result.response, 0)
	if result.response.statusCode == http.StatusTooManyRequests || result.response.statusCode == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[CreateOrganizationOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "organization no-send checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[CreateOrganizationOutput](result.failure, result.retryAfter)
	}
	if result.outcome == exchangeSucceeded && result.response.statusCode == http.StatusCreated {
		organization, decodeErr := decodeOrganizationEnvelope(result.response.body)
		if decodeErr == nil && matchesCreatedOrganization(organization, input) {
			_ = fence.RecordProviderID(strconv.FormatInt(organization.ID, 10))
			return sdkgo.NewMutationBranch(CreateOrganizationBranchCreated, CreateOrganizationOutput{Organization: organization}, nil, operation.client.receipt(call, result.response, organization.ID))
		}
	}
	if decision.MaximumPriorSends == 0 {
		if result.outcome == exchangeRejected || result.outcome == exchangeNotFound {
			return sdkgo.NewMutationBranch(CreateOrganizationBranchProviderRejected, CreateOrganizationOutput{}, &result.failure, receipt)
		}
		if result.outcome == exchangeDefect {
			return sdkgo.NewMutationBranch(CreateOrganizationBranchDefect, CreateOrganizationOutput{}, &result.failure, receipt)
		}
	}
	if decision.MaximumPriorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[CreateOrganizationOutput](zendeskFailure(sdkgo.FailureTransport, operationName, "organization create outcome is unconfirmed"), result.retryAfter)
	}
	return sdkgo.NewMutationUncertain(CreateOrganizationOutput{}, zendeskFailure(sdkgo.FailureTransport, operationName, "organization may have been created"), receipt)
}

// Invoke replaces selected organization fields by ID.
func (operation UpdateOrganizationOperation) Invoke(call sdkgo.Call, input UpdateOrganizationInput) sdkgo.MutationAttempt[UpdateOrganizationOutput] {
	const operationName = "updateOrganization"
	if input.OrganizationID < 1 || input.Name == nil && input.DomainNames == nil && input.Tags == nil && input.GroupID == nil && input.SharedTickets == nil && input.SharedComments == nil || input.Name != nil && strings.TrimSpace(*input.Name) == "" || input.GroupID != nil && *input.GroupID < 1 || !validOrganizationDomains(input.DomainNames) {
		return sdkgo.NewMutationBranch(UpdateOrganizationBranchDefect, UpdateOrganizationOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "organizationId and valid replacement fields are required"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateOrganizationBranchDefect, UpdateOrganizationOutput{}, failure, sdkgo.Receipt{})
	}
	fields := map[string]any{}
	if input.Name != nil {
		fields["name"] = *input.Name
	}
	if input.DomainNames != nil {
		fields["domain_names"] = input.DomainNames
	}
	if input.Tags != nil {
		fields["tags"] = input.Tags
	}
	if input.GroupID != nil {
		fields["group_id"] = *input.GroupID
	}
	if input.SharedTickets != nil {
		fields["shared_tickets"] = *input.SharedTickets
	}
	if input.SharedComments != nil {
		fields["shared_comments"] = *input.SharedComments
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodPut, path: organizationPath(input.OrganizationID), payload: map[string]any{"organization": fields}})
	receipt := operation.client.receipt(call, result.response, input.OrganizationID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewMutationRetry[UpdateOrganizationOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateOrganizationBranchNotFound, UpdateOrganizationOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateOrganizationBranchDefect, UpdateOrganizationOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateOrganizationBranchProviderRejected, UpdateOrganizationOutput{}, &result.failure, receipt)
	}
	organization, err := decodeOrganizationEnvelope(result.response.body)
	if err != nil || organization.ID != input.OrganizationID {
		return sdkgo.NewMutationRetry[UpdateOrganizationOutput](zendeskFailure(sdkgo.FailureProtocol, operationName, "updated organization response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateOrganizationBranchUpdated, UpdateOrganizationOutput{Organization: organization}, nil, receipt)
}

// Invoke deletes one organization by ID.
func (operation DeleteOrganizationOperation) Invoke(call sdkgo.Call, input DeleteOrganizationInput) sdkgo.MutationAttempt[DeleteOrganizationOutput] {
	const operationName = "deleteOrganization"
	if input.OrganizationID < 1 {
		return sdkgo.NewMutationBranch(DeleteOrganizationBranchDefect, DeleteOrganizationOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "organizationId must be positive"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteOrganizationBranchDefect, DeleteOrganizationOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodDelete, path: organizationPath(input.OrganizationID)})
	receipt := operation.client.receipt(call, result.response, input.OrganizationID)
	switch result.outcome {
	case exchangeSucceeded:
		if result.response.statusCode != http.StatusNoContent {
			return sdkgo.NewMutationRetry[DeleteOrganizationOutput](zendeskFailure(sdkgo.FailureProtocol, operationName, "organization delete response is invalid"), 0)
		}
		return sdkgo.NewMutationBranch(DeleteOrganizationBranchDeleted, DeleteOrganizationOutput{OrganizationID: input.OrganizationID}, nil, receipt)
	case exchangeRetryable:
		return sdkgo.NewMutationRetry[DeleteOrganizationOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(DeleteOrganizationBranchNotFound, DeleteOrganizationOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(DeleteOrganizationBranchDefect, DeleteOrganizationOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(DeleteOrganizationBranchProviderRejected, DeleteOrganizationOutput{}, &result.failure, receipt)
	}
}

func matchesCreatedOrganization(organization Organization, input CreateOrganizationInput) bool {
	return strings.EqualFold(organization.ExternalID, input.ExternalID) && organization.Name == input.Name
}

func validOrganizationDomains(domains []string) bool {
	for _, domain := range domains {
		if strings.TrimSpace(domain) == "" || strings.ContainsAny(domain, " \r\n\t") || len(domain) > 255 {
			return false
		}
	}
	return true
}
