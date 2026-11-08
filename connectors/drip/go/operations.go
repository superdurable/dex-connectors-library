// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drip

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListAccountsInput selects accounts accessible to the connected Drip user.
type ListAccountsInput struct{}

// AccountMetadata identifies one Drip account without private settings.
type AccountMetadata struct {
	// ID is the stable Drip account identifier.
	ID string `json:"id"`
	// Name is the account display name.
	Name string `json:"name"`
}

// AccountsList contains accessible account identities.
type AccountsList struct {
	// Accounts contains only IDs and display names.
	Accounts []AccountMetadata `json:"accounts"`
}

// GetSubscriberInput selects one subscriber by ID or email in an account.
type GetSubscriberInput struct {
	// AccountID is a numeric ID returned by ListAccounts.
	AccountID string `json:"accountId"`
	// SubscriberIDOrEmail is the Drip subscriber ID or email address.
	SubscriberIDOrEmail string `json:"subscriberIdOrEmail"`
}

// SubscriberMetadata contains subscriber identity and status without custom fields.
type SubscriberMetadata struct {
	// ID is the stable Drip subscriber identifier.
	ID string `json:"id"`
	// Email is the subscriber email address.
	Email string `json:"email"`
	// FirstName is the given name when present.
	FirstName string `json:"firstName"`
	// LastName is the surname when present.
	LastName string `json:"lastName"`
	// Status is the current subscriber status.
	Status string `json:"status"`
}

// ListAccountsOperation retrieves account IDs and names.
type ListAccountsOperation struct{ client *Client }

// GetSubscriberOperation retrieves one subscriber's identity and status.
type GetSubscriberOperation struct{ client *Client }

// ListAccounts binds account discovery to this client.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// GetSubscriber binds the subscriber Query to this client.
func (client *Client) GetSubscriber() GetSubscriberOperation { return GetSubscriberOperation{client} }

// Definition returns the generated account Query contract.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Definition returns the generated subscriber Query contract.
func (GetSubscriberOperation) Definition() sdkgo.QueryDefinition { return GetSubscriberDefinition }

// Invoke reads all account identities accessible to the connected user.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, _ ListAccountsInput) sdkgo.QueryAttempt[AccountsList] {
	branches := queryBranches{found: ListAccountsBranchListed, rejected: ListAccountsBranchProviderRejected, invalid: ListAccountsBranchInvalidResponse, defect: ListAccountsBranchDefect}
	return invokeQuery(call, operation.client, "listAccounts", "/accounts", branches, decodeAccounts)
}

// Invoke reads one Drip subscriber, excluding tracking data and custom fields.
func (operation GetSubscriberOperation) Invoke(call sdkgo.Call, input GetSubscriberInput) sdkgo.QueryAttempt[SubscriberMetadata] {
	branches := queryBranches{found: GetSubscriberBranchFound, notFound: GetSubscriberBranchNotFound, rejected: GetSubscriberBranchProviderRejected, invalid: GetSubscriberBranchInvalidResponse, defect: GetSubscriberBranchDefect}
	accountID, err := strconv.ParseUint(input.AccountID, 10, 64)
	if err != nil || accountID == 0 || input.SubscriberIDOrEmail == "" || strings.TrimSpace(input.SubscriberIDOrEmail) != input.SubscriberIDOrEmail || len(input.SubscriberIDOrEmail) > 512 || !utf8.ValidString(input.SubscriberIDOrEmail) {
		return queryBranch("getSubscriber", branches.defect, SubscriberMetadata{}, sdkgo.FailureValidation, "account ID or subscriber identifier is invalid")
	}
	path := "/" + input.AccountID + "/subscribers/" + url.PathEscape(input.SubscriberIDOrEmail)
	return invokeQuery(call, operation.client, "getSubscriber", path, branches, decodeSubscriber)
}

func decodeAccounts(body []byte) (AccountsList, error) {
	var wire struct {
		Accounts []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AccountsList{}, err
	}
	if wire.Accounts == nil {
		return AccountsList{}, fmt.Errorf("account list missing")
	}
	accounts := make([]AccountMetadata, 0, len(wire.Accounts))
	for _, account := range wire.Accounts {
		if account.ID == "" || account.Name == "" {
			return AccountsList{}, fmt.Errorf("account identity missing")
		}
		accounts = append(accounts, AccountMetadata{ID: account.ID, Name: account.Name})
	}
	return AccountsList{Accounts: accounts}, nil
}

func decodeSubscriber(body []byte) (SubscriberMetadata, error) {
	var wire struct {
		Subscribers []struct {
			ID        string `json:"id"`
			Email     string `json:"email"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Status    string `json:"status"`
		} `json:"subscribers"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SubscriberMetadata{}, err
	}
	if len(wire.Subscribers) != 1 {
		return SubscriberMetadata{}, fmt.Errorf("expected one subscriber")
	}
	subscriber := wire.Subscribers[0]
	if subscriber.ID == "" || subscriber.Email == "" || subscriber.Status == "" {
		return SubscriberMetadata{}, fmt.Errorf("subscriber identity missing")
	}
	return SubscriberMetadata{ID: subscriber.ID, Email: subscriber.Email, FirstName: subscriber.FirstName, LastName: subscriber.LastName, Status: subscriber.Status}, nil
}
