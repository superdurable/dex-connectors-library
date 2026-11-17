// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateIdeaInput creates an idea in an organization visible to the API key.
type CreateIdeaInput struct {
	// OrganizationID is returned by ListOrganizations.
	OrganizationID string `json:"organizationId"`
	// Title is the nonempty idea title.
	Title string `json:"title"`
	// Text is the nonempty idea body.
	Text string `json:"text"`
}

// IdeaProfile contains the created idea's identity and text content.
type IdeaProfile struct {
	// ID is Buffer's idea identifier.
	ID string `json:"id"`
	// Title is the idea title.
	Title string `json:"title"`
	// Text is the idea body.
	Text string `json:"text"`
}

// CreateIdeaOperation creates an idea with bounded sends.
type CreateIdeaOperation struct{ client *Client }

// CreateIdea binds idea creation to this client.
func (client *Client) CreateIdea() CreateIdeaOperation { return CreateIdeaOperation{client: client} }

// Definition returns the generated idea creation contract.
func (CreateIdeaOperation) Definition() sdkgo.MutationDefinition { return CreateIdeaDefinition }

// IdempotencyKey preserves the identity of bounded idea creation attempts.
func (CreateIdeaOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIdeaInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

const createIdeaGraphQL = "mutation CreateIdea($input: CreateIdeaInput!) { createIdea(input: $input) { ... on Idea { id content { title text } } } }"

// Invoke creates one text idea and reports an unknown outcome at the send cap.
func (operation CreateIdeaOperation) Invoke(call sdkgo.Call, input CreateIdeaInput) sdkgo.MutationAttempt[IdeaProfile] {
	const name = "createIdea"
	if !validGraphQLID(input.OrganizationID) || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Text) == "" {
		return bufferMutationBranch(CreateIdeaBranchDefect, IdeaProfile{}, name, sdkgo.FailureValidation, "organization ID, title, and text are required")
	}
	fence, err := singledispatch.NewFence(call, CreateIdeaDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return bufferMutationBranch(CreateIdeaBranchDefect, IdeaProfile{}, name, sdkgo.FailureLocalDefect, "idea dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(IdeaProfile{}, bufferFailure(name, sdkgo.FailureTransport, "an earlier idea may have been created"), sdkgo.Receipt{})
	}
	variables := map[string]any{"input": map[string]any{"organizationId": input.OrganizationID, "content": map[string]any{"title": input.Title, "text": input.Text}}}
	var response providerResponse
	err = fence.Dispatch(func(ctx context.Context) error {
		response = operation.client.executeGraphQLWithContext(ctx, call, createIdeaGraphQL, variables)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[IdeaProfile](bufferFailure(name, sdkgo.FailureAvailability, "idea dispatch was not recorded or sent"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[IdeaProfile](bufferFailure(name, sdkgo.FailureAvailability, "idea retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[IdeaProfile](bufferFailure(name, kind, "Buffer asked to retry idea creation"), response.retryAfter)
	}
	if err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 || isUnconfirmedIdeaResponse(response) {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(IdeaProfile{}, bufferFailure(name, sdkgo.FailureTransport, "idea outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[IdeaProfile](bufferFailure(name, sdkgo.FailureTransport, "idea outcome is unknown"), 0)
	}
	if attempt := classifyBufferMutationResponse[IdeaProfile](response, name, CreateIdeaBranchProviderRejected, CreateIdeaBranchProviderRejected, CreateIdeaBranchDefect); attempt != nil {
		return *attempt
	}
	var wire struct {
		Data *struct {
			CreateIdea *struct {
				ID      string `json:"id"`
				Content struct {
					Title string `json:"title"`
					Text  string `json:"text"`
				} `json:"content"`
			} `json:"createIdea"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || wire.Data.CreateIdea == nil || len(wire.Errors) != 0 {
		return bufferMutationBranch(CreateIdeaBranchProviderRejected, IdeaProfile{}, name, sdkgo.FailureProviderRejection, "Buffer rejected idea creation")
	}
	idea := wire.Data.CreateIdea
	if idea.ID == "" || idea.Content.Title != input.Title || idea.Content.Text != input.Text {
		return bufferMutationBranch(CreateIdeaBranchProviderRejected, IdeaProfile{}, name, sdkgo.FailureProtocol, "Buffer returned an unexpected idea")
	}
	return sdkgo.NewMutationBranch(CreateIdeaBranchCreated, IdeaProfile{ID: idea.ID, Title: idea.Content.Title, Text: idea.Content.Text}, nil, sdkgo.Receipt{})
}

func isUnconfirmedIdeaResponse(response providerResponse) bool {
	if response.status != http.StatusOK {
		return false
	}
	var wire struct {
		Data *struct {
			CreateIdea *struct {
				ID string `json:"id"`
			} `json:"createIdea"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		return true
	}
	return len(wire.Errors) == 0 && (wire.Data == nil || wire.Data.CreateIdea == nil || wire.Data.CreateIdea.ID == "")
}
