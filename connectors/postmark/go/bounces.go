// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postmark

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Bounce contains non-secret Postmark delivery failure metadata.
type Bounce struct {
	// ID is the provider bounce identity.
	ID int64 `json:"ID"`
	// Type classifies the delivery failure.
	Type string `json:"Type"`
	// Email is the affected recipient address.
	Email string `json:"Email"`
	// Inactive reports whether Postmark deactivated the recipient.
	Inactive bool `json:"Inactive"`
	// CanActivate reports whether Postmark permits reactivation.
	CanActivate bool `json:"CanActivate"`
	// MessageID is the associated outbound message identity.
	MessageID string `json:"MessageID,omitempty"`
}

// BouncePage contains one bounded page of delivery failures.
type BouncePage struct {
	// TotalCount is the number of matching bounces.
	TotalCount int `json:"TotalCount"`
	// Bounces contains the current page.
	Bounces []Bounce `json:"Bounces"`
}

// ListBouncesInput selects a bounded bounce page.
type ListBouncesInput struct {
	// Count is the page size, up to 500; zero selects 50.
	Count int `json:"count,omitempty"`
	// Offset is the zero-based page offset.
	Offset int `json:"offset,omitempty"`
}

// GetBounceInput identifies one bounce.
type GetBounceInput struct {
	// BounceID is the numeric provider identity.
	BounceID int64 `json:"bounceId"`
}

// ActivateBounceInput identifies a bounce whose recipient should be reactivated.
type ActivateBounceInput struct {
	// BounceID is the numeric provider identity.
	BounceID int64 `json:"bounceId"`
}

// ListBouncesOperation lists delivery failures.
type ListBouncesOperation struct{ client *Client }

// GetBounceOperation retrieves one delivery failure.
type GetBounceOperation struct{ client *Client }

// ActivateBounceOperation reactivates a bounce recipient.
type ActivateBounceOperation struct{ client *Client }

// ListBounces binds the list operation to this client.
func (client *Client) ListBounces() ListBouncesOperation { return ListBouncesOperation{client} }

// GetBounce binds the detail operation to this client.
func (client *Client) GetBounce() GetBounceOperation { return GetBounceOperation{client} }

// ActivateBounce binds the reactivation operation to this client.
func (client *Client) ActivateBounce() ActivateBounceOperation {
	return ActivateBounceOperation{client}
}

// Definition returns the generated listBounces contract.
func (ListBouncesOperation) Definition() sdkgo.QueryDefinition { return ListBouncesDefinition }

// Definition returns the generated getBounce contract.
func (GetBounceOperation) Definition() sdkgo.QueryDefinition { return GetBounceDefinition }

// Definition returns the generated activateBounce contract.
func (ActivateBounceOperation) Definition() sdkgo.MutationDefinition { return ActivateBounceDefinition }

// IdempotencyKey identifies the absolute recipient activation.
func (ActivateBounceOperation) IdempotencyKey(callID sdkgo.CallID, _ ActivateBounceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists a bounded bounce page.
func (operation ListBouncesOperation) Invoke(call sdkgo.Call, input ListBouncesInput) sdkgo.QueryAttempt[BouncePage] {
	if input.Count == 0 {
		input.Count = 50
	}
	if input.Count < 1 || input.Count > 500 || input.Offset < 0 || input.Count+input.Offset > 10000 {
		return queryFailure[BouncePage]("listBounces", ListBouncesBranchDefect, sdkgo.FailureValidation, "count and offset must fit Postmark's 10,000-bounce window")
	}
	query := url.Values{"count": {strconv.Itoa(input.Count)}, "offset": {strconv.Itoa(input.Offset)}}
	return runQuery(operation.client, call, "listBounces", "/bounces?"+query.Encode(), ListBouncesBranchListed, "", ListBouncesBranchProviderRejected, ListBouncesBranchInvalidResponse, ListBouncesBranchDefect, func(body []byte) (BouncePage, error) {
		var page BouncePage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Bounces == nil {
			err = fmt.Errorf("bounces missing")
		}
		return page, err
	})
}

// Invoke retrieves one bounce by provider identity.
func (operation GetBounceOperation) Invoke(call sdkgo.Call, input GetBounceInput) sdkgo.QueryAttempt[Bounce] {
	if input.BounceID <= 0 {
		return queryFailure[Bounce]("getBounce", GetBounceBranchDefect, sdkgo.FailureValidation, "positive bounce ID is required")
	}
	return runQuery(operation.client, call, "getBounce", "/bounces/"+strconv.FormatInt(input.BounceID, 10), GetBounceBranchFound, "", GetBounceBranchProviderRejected, GetBounceBranchInvalidResponse, GetBounceBranchDefect, decodeBounce)
}

// Invoke reactivates the recipient affected by one bounce.
func (operation ActivateBounceOperation) Invoke(call sdkgo.Call, input ActivateBounceInput) sdkgo.MutationAttempt[Bounce] {
	if input.BounceID <= 0 {
		return mutationFailure[Bounce]("activateBounce", ActivateBounceBranchDefect, sdkgo.FailureValidation, "positive bounce ID is required")
	}
	return runMutation(operation.client, call, "activateBounce", http.MethodPut, "/bounces/"+strconv.FormatInt(input.BounceID, 10)+"/activate", []byte{}, "", "", ActivateBounceBranchActivated, "", ActivateBounceBranchProviderRejected, ActivateBounceBranchInvalidResponse, ActivateBounceBranchDefect, func(body []byte) (Bounce, error) {
		var envelope struct {
			Bounce Bounce `json:"Bounce"`
		}
		err := json.Unmarshal(body, &envelope)
		if err == nil && envelope.Bounce.ID <= 0 {
			err = fmt.Errorf("activated bounce missing")
		}
		return envelope.Bounce, err
	})
}

func decodeBounce(body []byte) (Bounce, error) {
	var bounce Bounce
	err := json.Unmarshal(body, &bounce)
	if err == nil && bounce.ID <= 0 {
		err = fmt.Errorf("bounce ID missing")
	}
	return bounce, err
}
