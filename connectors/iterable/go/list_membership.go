// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iterable

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// SubscribeUserToListInput selects a static list and one user.
type SubscribeUserToListInput struct {
	// ListID is the positive numeric ID shown under Audience > Lists.
	ListID int64 `json:"listId"`
	// Email identifies an email-based or hybrid project user.
	Email string `json:"email,omitempty"`
	// UserID identifies a user ID-based or hybrid project user.
	UserID string `json:"userId,omitempty"`
}

// UnsubscribeUserFromListInput selects a static list and one user.
type UnsubscribeUserFromListInput struct {
	// ListID is the positive numeric ID shown under Audience > Lists.
	ListID int64 `json:"listId"`
	// Email identifies an email-based or hybrid project user.
	Email string `json:"email,omitempty"`
	// UserID identifies a user ID-based or hybrid project user.
	UserID string `json:"userId,omitempty"`
	// CampaignID optionally attributes the unsubscribe to a positive campaign ID.
	CampaignID int64 `json:"campaignId,omitempty"`
	// ChannelUnsubscribe also removes channel-level consent when true.
	ChannelUnsubscribe bool `json:"channelUnsubscribe,omitempty"`
}

// SubscribeUserToListOperation adds one user to a static list.
type SubscribeUserToListOperation struct{ client *Client }

// UnsubscribeUserFromListOperation removes one user from a static list.
type UnsubscribeUserFromListOperation struct{ client *Client }

// SubscribeUserToList binds this mutation to the client.
func (client *Client) SubscribeUserToList() SubscribeUserToListOperation {
	return SubscribeUserToListOperation{client}
}

// UnsubscribeUserFromList binds this mutation to the client.
func (client *Client) UnsubscribeUserFromList() UnsubscribeUserFromListOperation {
	return UnsubscribeUserFromListOperation{client}
}

// Definition returns the manifest-generated mutation contract.
func (SubscribeUserToListOperation) Definition() sdkgo.MutationDefinition {
	return SubscribeUserToListDefinition
}

// Definition returns the manifest-generated mutation contract.
func (UnsubscribeUserFromListOperation) Definition() sdkgo.MutationDefinition {
	return UnsubscribeUserFromListDefinition
}

// IdempotencyKey identifies one stable subscription across Dex retries.
func (SubscribeUserToListOperation) IdempotencyKey(callID sdkgo.CallID, _ SubscribeUserToListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies one stable unsubscription across Dex retries.
func (UnsubscribeUserFromListOperation) IdempotencyKey(callID sdkgo.CallID, _ UnsubscribeUserFromListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds the user to the list; repeating the same membership is harmless.
func (operation SubscribeUserToListOperation) Invoke(call sdkgo.Call, input SubscribeUserToListInput) sdkgo.MutationAttempt[UserWriteResult] {
	if input.ListID <= 0 || !validUserSelection(input.Email, input.UserID) {
		return userWriteBranch("subscribeUserToList", SubscribeUserToListBranchDefect, sdkgo.FailureValidation, "positive list ID and one valid user identity are required")
	}
	return operation.client.changeListMembership(call, "subscribeUserToList", "/lists/subscribe", input.ListID, input.Email, input.UserID, 0, false, SubscribeUserToListBranchSubscribed, SubscribeUserToListBranchProviderRejected, SubscribeUserToListBranchInvalidResponse, SubscribeUserToListBranchDefect)
}

// Invoke removes the user from the list; repeating the same removal is harmless.
func (operation UnsubscribeUserFromListOperation) Invoke(call sdkgo.Call, input UnsubscribeUserFromListInput) sdkgo.MutationAttempt[UserWriteResult] {
	if input.ListID <= 0 || !validUserSelection(input.Email, input.UserID) || input.CampaignID < 0 {
		return userWriteBranch("unsubscribeUserFromList", UnsubscribeUserFromListBranchDefect, sdkgo.FailureValidation, "positive list ID, one valid user identity, and nonnegative campaign ID are required")
	}
	return operation.client.changeListMembership(call, "unsubscribeUserFromList", "/lists/unsubscribe", input.ListID, input.Email, input.UserID, input.CampaignID, input.ChannelUnsubscribe, UnsubscribeUserFromListBranchUnsubscribed, UnsubscribeUserFromListBranchProviderRejected, UnsubscribeUserFromListBranchInvalidResponse, UnsubscribeUserFromListBranchDefect)
}

func (client *Client) changeListMembership(call sdkgo.Call, operationName, path string, listID int64, email, userID string, campaignID int64, channelUnsubscribe bool, successBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[UserWriteResult] {
	subscriber := map[string]string{}
	if email != "" {
		subscriber["email"] = email
	} else {
		subscriber["userId"] = userID
	}
	body := map[string]any{"listId": listID, "subscribers": []map[string]string{subscriber}}
	if campaignID != 0 {
		body["campaignId"] = campaignID
	}
	if channelUnsubscribe {
		body["channelUnsubscribe"] = true
	}
	requestBody, err := json.Marshal(body)
	if err != nil {
		return userWriteBranch(operationName, defectBranch, sdkgo.FailureLocalDefect, "Iterable list request could not be encoded")
	}
	response := client.request(call, http.MethodPost, path, requestBody)
	if response.status == http.StatusOK && response.err == nil {
		var result struct {
			SuccessCount *int `json:"successCount"`
			FailCount    *int `json:"failCount"`
		}
		if err := json.Unmarshal(response.body, &result); err == nil && result.SuccessCount != nil && result.FailCount != nil && *result.SuccessCount >= 0 && *result.FailCount >= 0 {
			if *result.SuccessCount == 1 && *result.FailCount == 0 {
				response.body = []byte(`{"code":"Success"}`)
			} else {
				response.body = []byte(`{"code":"Rejected"}`)
			}
		} else {
			response.body = []byte(`{}`)
		}
	}
	return classifyUserWrite(response, operationName, successBranch, "", rejectedBranch, invalidBranch, defectBranch)
}
