// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetBalanceInput selects one Wise balance account.
type GetBalanceInput struct {
	// ProfileID is a positive decimal Wise profile ID.
	ProfileID string `json:"profileId"`
	// BalanceID is a positive decimal Wise balance ID.
	BalanceID string `json:"balanceId"`
}

// CreateBalanceInput opens a standard or savings balance.
type CreateBalanceInput struct {
	// ProfileID is a positive decimal Wise profile ID.
	ProfileID string `json:"profileId"`
	// Currency is the ISO 4217 currency to hold.
	Currency string `json:"currency"`
	// Type is STANDARD or SAVINGS.
	Type string `json:"type"`
	// Name is required for SAVINGS and omitted for STANDARD.
	Name string `json:"name,omitempty"`
}

// GetBalanceOperation reads one balance account.
type GetBalanceOperation struct{ client *Client }

// GetBalance binds the Query to this client.
func (client *Client) GetBalance() GetBalanceOperation { return GetBalanceOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetBalanceOperation) Definition() sdkgo.QueryDefinition { return GetBalanceDefinition }

// Invoke gets one balance without bank account details.
func (operation GetBalanceOperation) Invoke(call sdkgo.Call, input GetBalanceInput) sdkgo.QueryAttempt[BalanceSummary] {
	if !validTransferID(input.ProfileID) || !validTransferID(input.BalanceID) {
		return wiseReadBranch[BalanceSummary]("getBalance", sdkgo.DefectBranchID, sdkgo.FailureValidation, "profile or balance ID is invalid")
	}
	path := "/profiles/" + url.PathEscape(input.ProfileID) + "/balances/" + url.PathEscape(input.BalanceID)
	return readWiseJSON(call, operation.client, "getBalance", path, GetBalanceBranchFound, true, decodeBalance)
}

// CreateBalanceOperation opens a balance with Wise's documented idempotency header.
type CreateBalanceOperation struct{ client *Client }

// CreateBalance binds the Mutation to this client.
func (client *Client) CreateBalance() CreateBalanceOperation { return CreateBalanceOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateBalanceOperation) Definition() sdkgo.MutationDefinition { return CreateBalanceDefinition }

// IdempotencyKey reuses the stable Call ID for Wise's X-idempotence-uuid header.
func (CreateBalanceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateBalanceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke opens one balance and safely retries with the same idempotency UUID.
func (operation CreateBalanceOperation) Invoke(call sdkgo.Call, input CreateBalanceInput) sdkgo.MutationAttempt[BalanceSummary] {
	if !validTransferID(input.ProfileID) || !validCurrency(input.Currency) || (input.Type != "STANDARD" && input.Type != "SAVINGS") || (input.Type == "SAVINGS" && strings.TrimSpace(input.Name) == "") || (input.Type == "STANDARD" && input.Name != "") || len(input.Name) > 100 || !quoteUUID.MatchString(string(call.IdempotencyKey)) {
		return wiseMutationBranch[BalanceSummary]("createBalance", "defect", sdkgo.FailureValidation, "profile, currency, type, name, or call ID is invalid")
	}
	request := map[string]string{"currency": input.Currency, "type": input.Type}
	if input.Name != "" {
		request["name"] = input.Name
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return wiseMutationBranch[BalanceSummary]("createBalance", "defect", sdkgo.FailureLocalDefect, "Wise balance request could not be encoded")
	}
	path := "/profiles/" + url.PathEscape(input.ProfileID) + "/balances"
	response := operation.client.sendWithContextAndHeader(call, call.Context, http.MethodPost, path, requestBody, "X-idempotence-uuid", string(call.IdempotencyKey))
	if retry, ok := wiseMutationRetry[BalanceSummary]("createBalance", response); ok {
		return retry
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return wiseMutationBranch[BalanceSummary]("createBalance", "providerRejected", sdkgo.FailureProviderRejection, "Wise rejected balance creation")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return wiseMutationBranch[BalanceSummary]("createBalance", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wise balance response exceeded the configured limit")
	}
	balance, err := decodeBalance(response.body)
	if err != nil || balance.Currency != input.Currency || balance.Type != input.Type {
		return wiseMutationBranch[BalanceSummary]("createBalance", "invalidResponse", sdkgo.FailureProtocol, "Wise returned an invalid balance response")
	}
	return sdkgo.NewMutationBranch(CreateBalanceBranchCreated, balance, nil, sdkgo.Receipt{})
}

func decodeBalance(body []byte) (BalanceSummary, error) {
	var wire struct {
		ID       int64  `json:"id"`
		Currency string `json:"currency"`
		Type     string `json:"type"`
		Amount   struct {
			Value float64 `json:"value"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return BalanceSummary{}, err
	}
	if wire.ID <= 0 || !validCurrency(wire.Currency) || (wire.Type != "STANDARD" && wire.Type != "SAVINGS") || wire.Amount.Value < 0 {
		return BalanceSummary{}, fmt.Errorf("balance fields invalid")
	}
	return BalanceSummary{ID: strconv.FormatInt(wire.ID, 10), Currency: wire.Currency, Type: wire.Type, AvailableAmount: wire.Amount.Value}, nil
}
