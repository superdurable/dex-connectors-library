// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListCampaignSubscribersInput selects a campaign subscriber page.
type ListCampaignSubscribersInput struct {
	// AccountID is a numeric Drip account ID.
	AccountID string `json:"accountId"`
	// CampaignID is a numeric email series campaign ID.
	CampaignID string `json:"campaignId"`
	// Status is active, unsubscribed, or removed; blank selects active.
	Status string `json:"status,omitempty"`
	// Page is one-based; zero selects page one.
	Page int `json:"page,omitempty"`
	// PerPage is 1–1000; zero selects Drip's default.
	PerPage int `json:"perPage,omitempty"`
	// Direction is asc or desc by creation time; blank uses Drip's default.
	Direction string `json:"direction,omitempty"`
}

// SubscriberIdentityInput selects one subscriber by ID or email.
type SubscriberIdentityInput struct {
	// AccountID is a numeric Drip account ID.
	AccountID string `json:"accountId"`
	// SubscriberIDOrEmail is the stable Drip ID or email address.
	SubscriberIDOrEmail string `json:"subscriberIdOrEmail"`
}

// SubscriberCampaignIdentityInput selects one subscriber and campaign.
type SubscriberCampaignIdentityInput struct {
	// AccountID is a numeric Drip account ID.
	AccountID string `json:"accountId"`
	// SubscriberIDOrEmail is the stable Drip ID or email address.
	SubscriberIDOrEmail string `json:"subscriberIdOrEmail"`
	// CampaignID is a numeric email series campaign ID.
	CampaignID string `json:"campaignId"`
}

// SubscribeToCampaignInput subscribes one email address to an email series campaign.
type SubscribeToCampaignInput struct {
	// AccountID is a numeric Drip account ID.
	AccountID string `json:"accountId"`
	// CampaignID is a numeric email series campaign ID.
	CampaignID string `json:"campaignId"`
	// Email is the subscriber's email address and read-back identity.
	Email string `json:"email"`
}

// CampaignSubscription reports one email series campaign membership.
type CampaignSubscription struct {
	// ID is the subscription ID.
	ID string `json:"id"`
	// CampaignID is the email series campaign ID.
	CampaignID string `json:"campaignId"`
	// Status is active, unsubscribed, or removed.
	Status string `json:"status"`
}

// CampaignSubscriptionsPage is one subscriber's campaign memberships.
type CampaignSubscriptionsPage struct {
	// Subscriptions contains the memberships returned by Drip.
	Subscriptions []CampaignSubscription `json:"subscriptions"`
	// Page is the one-based returned page number.
	Page int `json:"page"`
	// TotalPages is the number of available pages.
	TotalPages int `json:"totalPages"`
	// TotalCount is the number of memberships.
	TotalCount int `json:"totalCount"`
}

// ListCampaignSubscribersOperation reads a campaign subscriber page.
type ListCampaignSubscribersOperation struct{ client *Client }

// ListSubscriberCampaignSubscriptionsOperation reads memberships for one subscriber.
type ListSubscriberCampaignSubscriptionsOperation struct{ client *Client }

// SubscribeToCampaignOperation subscribes one email address with bounded sends.
type SubscribeToCampaignOperation struct{ client *Client }

// RemoveSubscriberFromCampaignOperation removes one campaign membership.
type RemoveSubscriberFromCampaignOperation struct{ client *Client }

// UnsubscribeSubscriberFromAllOperation stops all mailings for one subscriber.
type UnsubscribeSubscriberFromAllOperation struct{ client *Client }

// ListCampaignSubscribers returns the typed page Query.
func (client *Client) ListCampaignSubscribers() ListCampaignSubscribersOperation {
	return ListCampaignSubscribersOperation{client}
}

// ListSubscriberCampaignSubscriptions returns the typed membership Query.
func (client *Client) ListSubscriberCampaignSubscriptions() ListSubscriberCampaignSubscriptionsOperation {
	return ListSubscriberCampaignSubscriptionsOperation{client}
}

// SubscribeToCampaign returns the typed subscription Mutation.
func (client *Client) SubscribeToCampaign() SubscribeToCampaignOperation {
	return SubscribeToCampaignOperation{client}
}

// RemoveSubscriberFromCampaign returns the typed removal Mutation.
func (client *Client) RemoveSubscriberFromCampaign() RemoveSubscriberFromCampaignOperation {
	return RemoveSubscriberFromCampaignOperation{client}
}

// UnsubscribeSubscriberFromAll returns the typed global unsubscribe Mutation.
func (client *Client) UnsubscribeSubscriberFromAll() UnsubscribeSubscriberFromAllOperation {
	return UnsubscribeSubscriberFromAllOperation{client}
}

// Definition returns the generated campaign subscriber page contract.
func (ListCampaignSubscribersOperation) Definition() sdkgo.QueryDefinition {
	return ListCampaignSubscribersDefinition
}

// Definition returns the generated subscriber membership contract.
func (ListSubscriberCampaignSubscriptionsOperation) Definition() sdkgo.QueryDefinition {
	return ListSubscriberCampaignSubscriptionsDefinition
}

// Definition returns the generated subscription contract.
func (SubscribeToCampaignOperation) Definition() sdkgo.MutationDefinition {
	return SubscribeToCampaignDefinition
}

// Definition returns the generated removal contract.
func (RemoveSubscriberFromCampaignOperation) Definition() sdkgo.MutationDefinition {
	return RemoveSubscriberFromCampaignDefinition
}

// Definition returns the generated global unsubscribe contract.
func (UnsubscribeSubscriberFromAllOperation) Definition() sdkgo.MutationDefinition {
	return UnsubscribeSubscriberFromAllDefinition
}

// IdempotencyKey records call identity; Drip does not deduplicate subscriptions.
func (SubscribeToCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ SubscribeToCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records call identity for state-converging removal.
func (RemoveSubscriberFromCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ SubscriberCampaignIdentityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records call identity for state-converging unsubscribe.
func (UnsubscribeSubscriberFromAllOperation) IdempotencyKey(callID sdkgo.CallID, _ SubscriberIdentityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one bounded page of campaign subscribers.
func (operation ListCampaignSubscribersOperation) Invoke(call sdkgo.Call, input ListCampaignSubscribersInput) sdkgo.QueryAttempt[SubscribersPage] {
	branches := queryBranches{found: ListCampaignSubscribersBranchListed, rejected: ListCampaignSubscribersBranchProviderRejected, invalid: ListCampaignSubscribersBranchInvalidResponse, defect: ListCampaignSubscribersBranchDefect}
	if !validAccountID(input.AccountID) || !validAccountID(input.CampaignID) || input.Page < 0 || input.PerPage < 0 || input.PerPage > 1000 || input.Status != "" && input.Status != "active" && input.Status != "unsubscribed" && input.Status != "removed" || input.Direction != "" && input.Direction != "asc" && input.Direction != "desc" {
		return queryBranch("listCampaignSubscribers", branches.defect, SubscribersPage{}, sdkgo.FailureValidation, "campaign subscriber options are invalid")
	}
	query := url.Values{}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	if input.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(input.PerPage))
	}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	if input.Direction != "" {
		query.Set("direction", input.Direction)
	}
	path := "/" + input.AccountID + "/campaigns/" + input.CampaignID + "/subscribers"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return invokeQuery(call, operation.client, "listCampaignSubscribers", path, branches, decodeSubscribersPage)
}

// Invoke reads one subscriber's email series memberships.
func (operation ListSubscriberCampaignSubscriptionsOperation) Invoke(call sdkgo.Call, input SubscriberIdentityInput) sdkgo.QueryAttempt[CampaignSubscriptionsPage] {
	branches := queryBranches{found: ListSubscriberCampaignSubscriptionsBranchListed, rejected: ListSubscriberCampaignSubscriptionsBranchProviderRejected, invalid: ListSubscriberCampaignSubscriptionsBranchInvalidResponse, defect: ListSubscriberCampaignSubscriptionsBranchDefect}
	if !validSubscriberIdentity(input) {
		return queryBranch("listSubscriberCampaignSubscriptions", branches.defect, CampaignSubscriptionsPage{}, sdkgo.FailureValidation, "subscriber identity is invalid")
	}
	return invokeQuery(call, operation.client, "listSubscriberCampaignSubscriptions", subscriberPath(input)+"/campaign_subscriptions", branches, decodeCampaignSubscriptionsPage)
}

// Invoke adds one subscriber, recovering by membership read-back after an unknown send.
func (operation SubscribeToCampaignOperation) Invoke(call sdkgo.Call, input SubscribeToCampaignInput) sdkgo.MutationAttempt[SubscriberMetadata] {
	if !validAccountID(input.AccountID) || !validAccountID(input.CampaignID) || !validSubscriberEmail(input.Email) {
		return subscribeFailure(SubscribeToCampaignBranchDefect, sdkgo.FailureValidation, "subscription identity is invalid")
	}
	fence, err := singledispatch.NewFence(call, SubscribeToCampaignDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return subscribeFailure(SubscribeToCampaignBranchDefect, sdkgo.FailureLocalDefect, "subscription fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return subscribeUncertain("subscription checkpoint cannot be trusted")
	}
	if decision.MaximumPriorSends > 0 {
		identity := SubscriberIdentityInput{AccountID: input.AccountID, SubscriberIDOrEmail: input.Email}
		body, status, retryAfter, readErr := operation.client.get(call, subscriberPath(identity))
		if readErr != nil || status == http.StatusTooManyRequests || status >= 500 {
			return subscribeRetry(sdkgo.FailureAvailability, "subscriber read-back is unavailable", retryAfter)
		}
		if status == http.StatusOK {
			subscriber, decodeErr := decodeSubscriber(body)
			if decodeErr != nil || subscriber.Email != input.Email {
				return subscribeRetry(sdkgo.FailureProtocol, "subscriber read-back is invalid", 0)
			}
			membershipBody, membershipStatus, membershipRetryAfter, membershipErr := operation.client.get(call, "/"+input.AccountID+"/subscribers/"+url.PathEscape(subscriber.ID)+"/campaign_subscriptions")
			if membershipErr != nil || membershipStatus == http.StatusTooManyRequests || membershipStatus >= 500 {
				return subscribeRetry(sdkgo.FailureAvailability, "campaign membership read-back is unavailable", membershipRetryAfter)
			}
			if membershipStatus != http.StatusOK {
				return subscribeRetry(sdkgo.FailureProtocol, "campaign membership read-back is inconclusive", 0)
			}
			memberships, decodeErr := decodeCampaignSubscriptionsPage(membershipBody)
			if decodeErr != nil || memberships.TotalPages > 1 {
				return subscribeRetry(sdkgo.FailureProtocol, "campaign membership read-back is incomplete", 0)
			}
			for _, membership := range memberships.Subscriptions {
				if membership.CampaignID == input.CampaignID && membership.Status == "active" {
					return sdkgo.NewMutationBranch(SubscribeToCampaignBranchSubscribed, subscriber, nil, sdkgo.Receipt{})
				}
			}
		} else if status != http.StatusNotFound {
			return subscribeRetry(sdkgo.FailureProtocol, "subscriber read-back is inconclusive", 0)
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return subscribeUncertain("subscription cannot be confirmed after the send cap")
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return subscribeUncertain("subscription cannot be confirmed after the send cap")
	}
	requestBody, err := json.Marshal(map[string]any{"subscribers": []map[string]string{{"email": input.Email}}})
	if err != nil {
		return subscribeFailure(SubscribeToCampaignBranchDefect, sdkgo.FailureValidation, "subscription input could not be encoded")
	}
	var body []byte
	var status int
	var retryAfter time.Duration
	var sendErr error
	err = fence.Dispatch(func(requestContext context.Context) error {
		body, status, retryAfter, sendErr = operation.client.sendWithContext(call, requestContext, http.MethodPost, "/"+input.AccountID+"/campaigns/"+input.CampaignID+"/subscribers", requestBody)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return subscribeRetry(sdkgo.FailureAvailability, "subscription was not dispatched", 0)
	}
	if err != nil {
		return subscribeUncertain("subscription dispatch was not allowed")
	}
	if errors.Is(sendErr, errCredential) {
		return subscribeFailure(SubscribeToCampaignBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if recordErr := fence.RecordCanResend(); recordErr != nil {
			return subscribeRetry(sdkgo.FailureAvailability, "subscription retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return subscribeRetry(kind, "Drip is temporarily unavailable", retryAfter)
	}
	if sendErr != nil || status >= 500 || status == http.StatusRequestTimeout {
		return subscribeRetry(sdkgo.FailureAvailability, "subscription outcome is unknown", retryAfter)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return subscribeFailure(SubscribeToCampaignBranchProviderRejected, sdkgo.FailureProviderRejection, "Drip rejected subscription with HTTP "+strconv.Itoa(status))
	}
	subscriber, decodeErr := decodeSubscriber(body)
	if decodeErr != nil || subscriber.Email != input.Email {
		return subscribeRetry(sdkgo.FailureProtocol, "Drip subscription confirmation is unusable", 0)
	}
	return sdkgo.NewMutationBranch(SubscribeToCampaignBranchSubscribed, subscriber, nil, sdkgo.Receipt{})
}

// Invoke removes one campaign membership; repeating the removal converges on the same state.
func (operation RemoveSubscriberFromCampaignOperation) Invoke(call sdkgo.Call, input SubscriberCampaignIdentityInput) sdkgo.MutationAttempt[SubscriberMetadata] {
	branches := mutationBranches{success: RemoveSubscriberFromCampaignBranchRemoved, rejected: RemoveSubscriberFromCampaignBranchProviderRejected, invalid: RemoveSubscriberFromCampaignBranchInvalidResponse, defect: RemoveSubscriberFromCampaignBranchDefect}
	identity := SubscriberIdentityInput{AccountID: input.AccountID, SubscriberIDOrEmail: input.SubscriberIDOrEmail}
	if !validSubscriberIdentity(identity) || !validAccountID(input.CampaignID) {
		return mutationBranch("removeSubscriberFromCampaign", branches.defect, SubscriberMetadata{}, sdkgo.FailureValidation, "subscriber or campaign ID is invalid")
	}
	return invokeMutation(call, operation.client, "removeSubscriberFromCampaign", http.MethodPost, subscriberPath(identity)+"/remove?campaign_id="+input.CampaignID, nil, branches, decodeSubscriber)
}

// Invoke ensures one subscriber is unsubscribed from all mailings.
func (operation UnsubscribeSubscriberFromAllOperation) Invoke(call sdkgo.Call, input SubscriberIdentityInput) sdkgo.MutationAttempt[SubscriberMetadata] {
	branches := mutationBranches{success: UnsubscribeSubscriberFromAllBranchUnsubscribed, rejected: UnsubscribeSubscriberFromAllBranchProviderRejected, invalid: UnsubscribeSubscriberFromAllBranchInvalidResponse, defect: UnsubscribeSubscriberFromAllBranchDefect}
	if !validSubscriberIdentity(input) {
		return mutationBranch("unsubscribeSubscriberFromAll", branches.defect, SubscriberMetadata{}, sdkgo.FailureValidation, "subscriber identity is invalid")
	}
	return invokeMutation(call, operation.client, "unsubscribeSubscriberFromAll", http.MethodPost, subscriberPath(input)+"/unsubscribe_all", nil, branches, decodeSubscriber)
}

func validSubscriberIdentity(input SubscriberIdentityInput) bool {
	return validAccountID(input.AccountID) && validSubscriberIdentifier(input.SubscriberIDOrEmail)
}
func subscriberPath(input SubscriberIdentityInput) string {
	return "/" + input.AccountID + "/subscribers/" + url.PathEscape(input.SubscriberIDOrEmail)
}

func decodeCampaignSubscriptionsPage(body []byte) (CampaignSubscriptionsPage, error) {
	var wire struct {
		Meta struct {
			Page       int `json:"page"`
			TotalPages int `json:"total_pages"`
			TotalCount int `json:"total_count"`
		} `json:"meta"`
		Subscriptions []struct {
			ID         string `json:"id"`
			CampaignID string `json:"campaign_id"`
			Status     string `json:"status"`
		} `json:"campaign_subscriptions"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CampaignSubscriptionsPage{}, err
	}
	if wire.Subscriptions == nil || wire.Meta.Page < 1 || wire.Meta.TotalPages < 0 || wire.Meta.TotalCount < 0 {
		return CampaignSubscriptionsPage{}, fmt.Errorf("campaign subscription page is incomplete")
	}
	page := CampaignSubscriptionsPage{Subscriptions: make([]CampaignSubscription, 0, len(wire.Subscriptions)), Page: wire.Meta.Page, TotalPages: wire.Meta.TotalPages, TotalCount: wire.Meta.TotalCount}
	for _, subscription := range wire.Subscriptions {
		if subscription.ID == "" || !validAccountID(subscription.CampaignID) || subscription.Status == "" {
			return CampaignSubscriptionsPage{}, fmt.Errorf("campaign subscription identity is incomplete")
		}
		page.Subscriptions = append(page.Subscriptions, CampaignSubscription{ID: subscription.ID, CampaignID: subscription.CampaignID, Status: subscription.Status})
	}
	return page, nil
}

func subscribeFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SubscriberMetadata] {
	failure := queryFailure("subscribeToCampaign", kind, message)
	return sdkgo.NewMutationBranch(branch, SubscriberMetadata{}, &failure, sdkgo.Receipt{})
}
func subscribeRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SubscriberMetadata] {
	return sdkgo.NewMutationRetry[SubscriberMetadata](queryFailure("subscribeToCampaign", kind, message), retryAfter)
}
func subscribeUncertain(message string) sdkgo.MutationAttempt[SubscriberMetadata] {
	return sdkgo.NewMutationUncertain(SubscriberMetadata{}, queryFailure("subscribeToCampaign", sdkgo.FailureAvailability, message), sdkgo.Receipt{})
}
