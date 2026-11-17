// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendgrid

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateListInput names the new SendGrid marketing list.
type CreateListInput struct {
	// Name is the nonblank list name shown in Marketing > Contacts > Lists.
	Name string `json:"name"`
}

// CreateListOperation creates one static marketing list.
type CreateListOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (CreateListOperation) Definition() sdkgo.MutationDefinition { return CreateListDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (CreateListOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// CreateList binds the operation to this client.
func (client *Client) CreateList() CreateListOperation { return CreateListOperation{client} }

// Invoke bounds duplicate creates and confirms a saved provider ID on retry.
func (operation CreateListOperation) Invoke(call sdkgo.Call, input CreateListInput) sdkgo.MutationAttempt[MarketingList] {
	client := operation.client
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 100 {
		return mutationBranch("createList", CreateListBranchDefect, MarketingList{}, sdkgo.FailureValidation, "list name is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateListDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationBranch("createList", CreateListBranchDefect, MarketingList{}, sdkgo.FailureLocalDefect, "list dispatch fence could not be built")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return uncertainList(call, "list create checkpoint could not be trusted")
	}
	if decision.Marker.ProviderID != "" {
		body, status, _, readErr := client.do(call, http.MethodGet, "/v3/marketing/lists/"+url.PathEscape(decision.Marker.ProviderID), nil, nil)
		if readErr == nil && status == http.StatusOK {
			list, decodeErr := decodeMarketingList(body)
			if decodeErr == nil && list.ID == decision.Marker.ProviderID && list.Name == input.Name {
				return sdkgo.NewMutationBranch(CreateListBranchCreated, list, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "sendgrid", ProviderObjectID: list.ID})
			}
		}
		return uncertainList(call, "previously created list could not be confirmed")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertainList(call, "an earlier list create may have succeeded")
	}
	payload, err := json.Marshal(map[string]string{"name": input.Name})
	if err != nil {
		return mutationBranch("createList", CreateListBranchDefect, MarketingList{}, sdkgo.FailureLocalDefect, "list payload could not be encoded")
	}
	var body []byte
	var status int
	var retryAfterDuration time.Duration
	err = fence.Dispatch(func(requestContext context.Context) error {
		var requestErr error
		body, status, retryAfterDuration, requestErr = client.doWithContext(requestContext, call, http.MethodPost, "/v3/marketing/lists", nil, payload)
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[MarketingList](queryFailure("createList", sdkgo.FailureAvailability, "list request was not sent"), 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable || errors.Is(err, errCredential) {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[MarketingList](queryFailure("createList", sdkgo.FailureAvailability, "list resend checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[MarketingList](queryFailure("createList", sdkgo.FailureAvailability, "list create was not processed"), retryAfterDuration)
	}
	if err != nil || status >= 500 {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[MarketingList](queryFailure("createList", sdkgo.FailureTransport, "list create outcome is unknown"), 0)
		}
		return uncertainList(call, "list create outcome is unknown after bounded sends")
	}
	if status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return uncertainList(call, "an earlier list create may have succeeded")
		}
		return mutationBranch("createList", CreateListBranchProviderRejected, MarketingList{}, sdkgo.FailureProviderRejection, "provider rejected list creation")
	}
	list, decodeErr := decodeMarketingList(body)
	if decodeErr != nil || list.Name != input.Name {
		return uncertainList(call, "provider returned an invalid created list")
	}
	if fence.RecordProviderID(list.ID) != nil {
		return sdkgo.NewMutationRetry[MarketingList](queryFailure("createList", sdkgo.FailureAvailability, "created list checkpoint failed"), 0)
	}
	return sdkgo.NewMutationBranch(CreateListBranchCreated, list, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "sendgrid", ProviderObjectID: list.ID})
}

func uncertainList(call sdkgo.Call, message string) sdkgo.MutationAttempt[MarketingList] {
	return sdkgo.NewMutationUncertain(MarketingList{}, queryFailure("createList", sdkgo.FailureTransport, message), sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "sendgrid"})
}
