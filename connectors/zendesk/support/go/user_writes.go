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

// CreateUserInput creates an end user with an account-unique application identifier.
type CreateUserInput struct {
	// Name is the end user's nonempty display name.
	Name string `json:"name"`
	// Email is one bare email address.
	Email string `json:"email"`
	// ExternalID is a stable account-unique identifier that enables exact read-back.
	ExternalID string `json:"externalId"`
}

// CreateUserOutput contains the created or confirmed end user.
type CreateUserOutput struct {
	// User is the end user Zendesk returned.
	User User `json:"user"`
}

// UpdateUserInput replaces selected fields of one end user.
type UpdateUserInput struct {
	// UserID is the positive Zendesk user ID.
	UserID int64 `json:"userId"`
	// Name replaces the display name when supplied.
	Name *string `json:"name,omitempty"`
	// Phone replaces the phone number when supplied.
	Phone *string `json:"phone,omitempty"`
	// Tags replaces all user tags when supplied.
	Tags []string `json:"tags,omitempty"`
	// Suspended replaces the suspension state when supplied.
	Suspended *bool `json:"suspended,omitempty"`
}

// UpdateUserOutput contains the updated user.
type UpdateUserOutput struct {
	// User is the user Zendesk returned after the update.
	User User `json:"user"`
}

// DeleteUserInput identifies a user for deletion.
type DeleteUserInput struct {
	// UserID is the positive Zendesk user ID.
	UserID int64 `json:"userId"`
}

// DeleteUserOutput identifies the deleted user.
type DeleteUserOutput struct {
	// UserID is the deleted user ID.
	UserID int64 `json:"userId"`
}

// CreateUserOperation creates one end user under a bounded send cap.
type CreateUserOperation struct{ client *Client }

// UpdateUserOperation updates one user with a repeatable replacement.
type UpdateUserOperation struct{ client *Client }

// DeleteUserOperation deletes one user.
type DeleteUserOperation struct{ client *Client }

// CreateUser binds the user create Mutation to this client.
func (client *Client) CreateUser() CreateUserOperation { return CreateUserOperation{client} }

// UpdateUser binds the user update Mutation to this client.
func (client *Client) UpdateUser() UpdateUserOperation { return UpdateUserOperation{client} }

// DeleteUser binds the user delete Mutation to this client.
func (client *Client) DeleteUser() DeleteUserOperation { return DeleteUserOperation{client} }

// Definition returns the generated create-user Mutation contract.
func (CreateUserOperation) Definition() sdkgo.MutationDefinition { return CreateUserDefinition }

// Definition returns the generated update-user Mutation contract.
func (UpdateUserOperation) Definition() sdkgo.MutationDefinition { return UpdateUserDefinition }

// Definition returns the generated delete-user Mutation contract.
func (DeleteUserOperation) Definition() sdkgo.MutationDefinition { return DeleteUserDefinition }

// IdempotencyKey correlates retries; Zendesk does not deduplicate user creates with this key.
func (CreateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates retries of one user update.
func (UpdateUserOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates retries of one user deletion.
func (DeleteUserOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one end user and reads back by its account-unique external ID after an unknown result.
func (operation CreateUserOperation) Invoke(call sdkgo.Call, input CreateUserInput) sdkgo.MutationAttempt[CreateUserOutput] {
	const operationName = "createUser"
	if strings.TrimSpace(input.Name) == "" || !isBareEmailAddress(input.Email) || strings.TrimSpace(input.ExternalID) == "" || len(input.ExternalID) > 255 || strings.ContainsAny(input.ExternalID, "\r\n\t") {
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "name, bare email, and a unique externalId are required"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateUserDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, zendeskFailurePointer(sdkgo.FailureLocalDefect, operationName, "user dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		if decision.Kind == singledispatch.SendAllowed {
			_ = fence.RecordNotSent()
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreateUserOutput{}, *failure, operation.client.receipt(call, zendeskResponse{}, 0))
		}
		return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, failure, sdkgo.Receipt{})
	}
	if decision.Marker.ProviderID != "" && decision.Marker.CallID == call.ID {
		read := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: userPathFromProviderID(decision.Marker.ProviderID)})
		if read.outcome == exchangeSucceeded {
			user, decodeErr := decodeUserEnvelope(read.response.body)
			if decodeErr == nil && matchesCreatedUser(user, input) {
				return sdkgo.NewMutationBranch(CreateUserBranchCreated, CreateUserOutput{User: user}, nil, operation.client.receipt(call, read.response, user.ID))
			}
		}
		return sdkgo.NewMutationRetry[CreateUserOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "created user read-back is unavailable"), 0)
	}
	if decision.MaximumPriorSends > 0 {
		query := url.Values{"external_id": {input.ExternalID}, "page[size]": {"2"}}
		read := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/users", query: query})
		if read.outcome == exchangeSucceeded {
			users, _, decodeErr := decodeUserPage(read.response.body, 2)
			if decodeErr != nil {
				return sdkgo.NewMutationRetry[CreateUserOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "user read-back is invalid"), 0)
			}
			for _, user := range users {
				if matchesCreatedUser(user, input) {
					return sdkgo.NewMutationBranch(CreateUserBranchCreated, CreateUserOutput{User: user}, nil, operation.client.receipt(call, read.response, user.ID))
				}
				if strings.EqualFold(user.ExternalID, input.ExternalID) {
					return sdkgo.NewMutationUncertain(CreateUserOutput{}, zendeskFailure(sdkgo.FailureConflict, operationName, "external ID belongs to an unconfirmed user"), operation.client.receipt(call, read.response, user.ID))
				}
			}
		} else {
			return sdkgo.NewMutationRetry[CreateUserOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "user read-back is unavailable"), read.retryAfter)
		}
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(CreateUserOutput{}, zendeskFailure(sdkgo.FailureTransport, operationName, "an earlier user create may have succeeded"), operation.client.receipt(call, zendeskResponse{}, 0))
	}
	var result zendeskExchange
	err = fence.Dispatch(func(ctx context.Context) error {
		result = operation.client.exchangeWithContext(ctx, credentials, operationName, zendeskRequest{method: http.MethodPost, path: "/users", payload: map[string]any{"user": map[string]any{"name": input.Name, "email": input.Email, "external_id": input.ExternalID, "role": "end-user"}}})
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CreateUserOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "user dispatch checkpoint is unavailable"), 0)
	}
	receipt := operation.client.receipt(call, result.response, 0)
	if result.response.statusCode == http.StatusTooManyRequests || result.response.statusCode == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[CreateUserOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "user no-send checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[CreateUserOutput](result.failure, result.retryAfter)
	}
	if result.outcome == exchangeSucceeded && result.response.statusCode == http.StatusCreated {
		user, decodeErr := decodeUserEnvelope(result.response.body)
		if decodeErr == nil && matchesCreatedUser(user, input) {
			_ = fence.RecordProviderID(strconv.FormatInt(user.ID, 10))
			return sdkgo.NewMutationBranch(CreateUserBranchCreated, CreateUserOutput{User: user}, nil, operation.client.receipt(call, result.response, user.ID))
		}
	}
	if decision.MaximumPriorSends == 0 {
		if result.outcome == exchangeRejected || result.outcome == exchangeNotFound {
			return sdkgo.NewMutationBranch(CreateUserBranchProviderRejected, CreateUserOutput{}, &result.failure, receipt)
		}
		if result.outcome == exchangeDefect {
			return sdkgo.NewMutationBranch(CreateUserBranchDefect, CreateUserOutput{}, &result.failure, receipt)
		}
	}
	if decision.MaximumPriorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[CreateUserOutput](zendeskFailure(sdkgo.FailureTransport, operationName, "user create outcome is unconfirmed"), result.retryAfter)
	}
	return sdkgo.NewMutationUncertain(CreateUserOutput{}, zendeskFailure(sdkgo.FailureTransport, operationName, "user may have been created"), receipt)
}

// Invoke replaces selected profile fields on an existing user.
func (operation UpdateUserOperation) Invoke(call sdkgo.Call, input UpdateUserInput) sdkgo.MutationAttempt[UpdateUserOutput] {
	const operationName = "updateUser"
	if input.UserID < 1 || (input.Name == nil && input.Phone == nil && input.Tags == nil && input.Suspended == nil) || input.Name != nil && strings.TrimSpace(*input.Name) == "" {
		return sdkgo.NewMutationBranch(UpdateUserBranchDefect, UpdateUserOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "positive userId and at least one valid profile field are required"), sdkgo.Receipt{})
	}
	if input.Tags != nil {
		for _, tag := range input.Tags {
			if strings.TrimSpace(tag) == "" || len(tag) > 255 {
				return sdkgo.NewMutationBranch(UpdateUserBranchDefect, UpdateUserOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "tags must be nonempty and at most 255 bytes"), sdkgo.Receipt{})
			}
		}
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateUserBranchDefect, UpdateUserOutput{}, failure, sdkgo.Receipt{})
	}
	fields := map[string]any{}
	if input.Name != nil {
		fields["name"] = *input.Name
	}
	if input.Phone != nil {
		fields["phone"] = *input.Phone
	}
	if input.Tags != nil {
		fields["tags"] = input.Tags
	}
	if input.Suspended != nil {
		fields["suspended"] = *input.Suspended
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodPut, path: userPath(input.UserID), payload: map[string]any{"user": fields}})
	receipt := operation.client.receipt(call, result.response, input.UserID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewMutationRetry[UpdateUserOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateUserBranchNotFound, UpdateUserOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateUserBranchDefect, UpdateUserOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateUserBranchProviderRejected, UpdateUserOutput{}, &result.failure, receipt)
	}
	user, err := decodeUserEnvelope(result.response.body)
	if err != nil || user.ID != input.UserID {
		return sdkgo.NewMutationRetry[UpdateUserOutput](zendeskFailure(sdkgo.FailureProtocol, operationName, "updated user response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateUserBranchUpdated, UpdateUserOutput{User: user}, nil, receipt)
}

// Invoke deletes one user; Zendesk does not recover a deleted user.
func (operation DeleteUserOperation) Invoke(call sdkgo.Call, input DeleteUserInput) sdkgo.MutationAttempt[DeleteUserOutput] {
	const operationName = "deleteUser"
	if input.UserID < 1 {
		return sdkgo.NewMutationBranch(DeleteUserBranchDefect, DeleteUserOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "userId must be positive"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteUserBranchDefect, DeleteUserOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodDelete, path: userPath(input.UserID)})
	receipt := operation.client.receipt(call, result.response, input.UserID)
	switch result.outcome {
	case exchangeSucceeded:
		if result.response.statusCode != http.StatusOK {
			return sdkgo.NewMutationRetry[DeleteUserOutput](zendeskFailure(sdkgo.FailureProtocol, operationName, "delete response status is unexpected"), 0)
		}
		var envelope struct {
			User struct {
				ID     int64 `json:"id"`
				Active *bool `json:"active"`
			} `json:"user"`
		}
		if err := json.Unmarshal(result.response.body, &envelope); err != nil || envelope.User.ID != input.UserID || envelope.User.Active == nil || *envelope.User.Active {
			return sdkgo.NewMutationRetry[DeleteUserOutput](zendeskFailure(sdkgo.FailureProtocol, operationName, "deleted user response is invalid"), 0)
		}
		return sdkgo.NewMutationBranch(DeleteUserBranchDeleted, DeleteUserOutput{UserID: input.UserID}, nil, receipt)
	case exchangeRetryable:
		return sdkgo.NewMutationRetry[DeleteUserOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(DeleteUserBranchNotFound, DeleteUserOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(DeleteUserBranchDefect, DeleteUserOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(DeleteUserBranchProviderRejected, DeleteUserOutput{}, &result.failure, receipt)
	}
}

func matchesCreatedUser(user User, input CreateUserInput) bool {
	return strings.EqualFold(user.ExternalID, input.ExternalID) && user.Name == input.Name && strings.EqualFold(user.Email, input.Email)
}

func userPathFromProviderID(providerID string) string { return "/users/" + url.PathEscape(providerID) }
