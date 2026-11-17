// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercel

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CancelDeploymentInput identifies a deployment and optional owning team.
type CancelDeploymentInput struct {
	// DeploymentID is the Vercel deployment ID, beginning dpl_.
	DeploymentID string `json:"deploymentId"`
	// TeamID selects a team; blank uses the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// DeleteDeploymentInput identifies a deployment and optional owning team.
type DeleteDeploymentInput struct {
	// DeploymentID is the Vercel deployment ID, beginning dpl_.
	DeploymentID string `json:"deploymentId"`
	// TeamID selects a team; blank uses the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// DeletedDeployment identifies the deployment that is now absent.
type DeletedDeployment struct {
	// DeploymentID is the requested Vercel deployment ID.
	DeploymentID string `json:"deploymentId"`
}

// CancelDeploymentOperation cancels one pending deployment.
type CancelDeploymentOperation struct{ client *Client }

// CancelDeployment binds cancellation to this client.
func (client *Client) CancelDeployment() CancelDeploymentOperation {
	return CancelDeploymentOperation{client}
}

// Definition returns the generated Mutation contract.
func (CancelDeploymentOperation) Definition() sdkgo.MutationDefinition {
	return CancelDeploymentDefinition
}

// IdempotencyKey identifies retries of the same cancellation.
func (CancelDeploymentOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelDeploymentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke asks Vercel to cancel a pending deployment.
func (operation CancelDeploymentOperation) Invoke(call sdkgo.Call, input CancelDeploymentInput) sdkgo.MutationAttempt[Deployment] {
	if !validDeploymentID(input.DeploymentID) || !validTeamID(input.TeamID) {
		return mutationFailure[Deployment]("cancelDeployment", "defect", sdkgo.FailureValidation, "Vercel deployment or team ID is invalid")
	}
	response, localFailure, err := operation.client.send(call, "cancelDeployment", requestSpec{
		method: http.MethodPatch, path: "/v12/deployments/" + url.PathEscape(input.DeploymentID) + "/cancel",
		query: teamQuery(input.TeamID),
	})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("defect"), Deployment{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return mutationRetry[Deployment]("cancelDeployment", sdkgo.FailureTransport, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		kind, delay := retryFailure(response, "cancelDeployment")
		return sdkgo.NewMutationRetry[Deployment](kind, delay)
	}
	if response.StatusCode == http.StatusNotFound {
		return mutationFailure[Deployment]("cancelDeployment", "notFound", sdkgo.FailureNotFound, "Vercel did not find the deployment")
	}
	if response.StatusCode != http.StatusOK {
		return mutationFailure[Deployment]("cancelDeployment", "providerRejected", sdkgo.FailureProviderRejection, "Vercel rejected cancellation")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return mutationFailure[Deployment]("cancelDeployment", "invalidResponse", sdkgo.FailureResponseTooLarge, "Vercel cancellation response was too large")
	}
	if err != nil {
		return mutationRetry[Deployment]("cancelDeployment", sdkgo.FailureTransport, 0)
	}
	deployment, err := decodeGetDeployment(body)
	if err != nil {
		return mutationFailure[Deployment]("cancelDeployment", "invalidResponse", sdkgo.FailureProtocol, "Vercel cancellation response was malformed")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("canceled"), deployment, nil, sdkgo.Receipt{})
}

// DeleteDeploymentOperation removes one deployment.
type DeleteDeploymentOperation struct{ client *Client }

// DeleteDeployment binds deletion to this client.
func (client *Client) DeleteDeployment() DeleteDeploymentOperation {
	return DeleteDeploymentOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteDeploymentOperation) Definition() sdkgo.MutationDefinition {
	return DeleteDeploymentDefinition
}

// IdempotencyKey identifies retries of the same deletion.
func (DeleteDeploymentOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteDeploymentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes the named deployment; an absent deployment already meets the target state.
func (operation DeleteDeploymentOperation) Invoke(call sdkgo.Call, input DeleteDeploymentInput) sdkgo.MutationAttempt[DeletedDeployment] {
	if !validDeploymentID(input.DeploymentID) || !validTeamID(input.TeamID) {
		return mutationFailure[DeletedDeployment]("deleteDeployment", "defect", sdkgo.FailureValidation, "Vercel deployment or team ID is invalid")
	}
	response, localFailure, err := operation.client.send(call, "deleteDeployment", requestSpec{
		method: http.MethodDelete, path: "/v13/deployments/" + url.PathEscape(input.DeploymentID), query: teamQuery(input.TeamID),
	})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("defect"), DeletedDeployment{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return mutationRetry[DeletedDeployment]("deleteDeployment", sdkgo.FailureTransport, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		kind, delay := retryFailure(response, "deleteDeployment")
		return sdkgo.NewMutationRetry[DeletedDeployment](kind, delay)
	}
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound {
		return mutationFailure[DeletedDeployment]("deleteDeployment", "providerRejected", sdkgo.FailureProviderRejection, "Vercel rejected deletion")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), DeletedDeployment{DeploymentID: input.DeploymentID}, nil, sdkgo.Receipt{})
}

func validDeploymentID(value string) bool {
	return strings.HasPrefix(value, "dpl_") && len(value) > 4 && len(value) <= 128 && !strings.ContainsAny(value, "/?# ")
}

func validTeamID(value string) bool {
	return value == "" || strings.HasPrefix(value, "team_") && len(value) > 5 && !strings.ContainsAny(value, "/?# ")
}

func teamQuery(teamID string) url.Values {
	query := url.Values{}
	if teamID != "" {
		query.Set("teamId", teamID)
	}
	return query
}

func mutationFailure[T any](operation, branch string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "vercel", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(sdkgo.BranchID(branch), *new(T), &failure, sdkgo.Receipt{})
}

func mutationRetry[T any](operation string, kind sdkgo.FailureKind, delay time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "vercel", Operation: operation, Message: "Vercel request could not be completed"}, delay)
}
