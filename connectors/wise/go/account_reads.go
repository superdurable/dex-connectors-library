// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListBalancesInput selects balance types within a Wise profile.
type ListBalancesInput struct {
	// ProfileID is a positive decimal Wise profile ID.
	ProfileID string `json:"profileId"`
	// Types is STANDARD, SAVINGS, or STANDARD,SAVINGS.
	Types string `json:"types"`
}

// BalanceSummary excludes bank account details and transaction history.
type BalanceSummary struct {
	// ID is the Wise balance account ID.
	ID string `json:"id"`
	// Currency is the ISO 4217 currency.
	Currency string `json:"currency"`
	// Type is STANDARD or SAVINGS.
	Type string `json:"type"`
	// AvailableAmount is the amount Wise marks available for transfers.
	AvailableAmount float64 `json:"availableAmount"`
}

// BalancesOutput contains sanitized balance summaries.
type BalancesOutput struct {
	// Balances is the list for the selected types.
	Balances []BalanceSummary `json:"balances"`
}

// ListBalancesOperation reads balance account summaries.
type ListBalancesOperation struct{ client *Client }

// ListBalances binds the Query to this client.
func (client *Client) ListBalances() ListBalancesOperation { return ListBalancesOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListBalancesOperation) Definition() sdkgo.QueryDefinition { return ListBalancesDefinition }

// Invoke lists balances without bank account details.
func (operation ListBalancesOperation) Invoke(call sdkgo.Call, input ListBalancesInput) sdkgo.QueryAttempt[BalancesOutput] {
	if !validTransferID(input.ProfileID) || (input.Types != "STANDARD" && input.Types != "SAVINGS" && input.Types != "STANDARD,SAVINGS") {
		return wiseReadBranch[BalancesOutput]("listBalances", sdkgo.DefectBranchID, sdkgo.FailureValidation, "profile ID or balance types are invalid")
	}
	query := url.Values{"types": {input.Types}}
	path := "/profiles/" + url.PathEscape(input.ProfileID) + "/balances?" + query.Encode()
	return readWiseJSON(call, operation.client, "listBalances", path, ListBalancesBranchListed, false, decodeBalances)
}

// ListPayinCurrenciesInput selects a Wise profile.
type ListPayinCurrenciesInput struct {
	// ProfileID is a positive decimal Wise profile ID.
	ProfileID string `json:"profileId"`
}

// PayinCurrenciesOutput lists currencies supporting balance bank account details.
type PayinCurrenciesOutput struct {
	// Currencies are ISO 4217 codes.
	Currencies []string `json:"currencies"`
}

// ListAvailableBalanceCurrenciesOperation reads supported balance currencies.
type ListAvailableBalanceCurrenciesOperation struct{ client *Client }

// ListAvailableBalanceCurrencies binds the Query to this client.
func (client *Client) ListAvailableBalanceCurrencies() ListAvailableBalanceCurrenciesOperation {
	return ListAvailableBalanceCurrenciesOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListAvailableBalanceCurrenciesOperation) Definition() sdkgo.QueryDefinition {
	return ListAvailableBalanceCurrenciesDefinition
}

// Invoke lists currencies in which a profile may hold a balance.
func (operation ListAvailableBalanceCurrenciesOperation) Invoke(call sdkgo.Call, input ListPayinCurrenciesInput) sdkgo.QueryAttempt[PayinCurrenciesOutput] {
	if !validTransferID(input.ProfileID) {
		return wiseReadBranch[PayinCurrenciesOutput]("listAvailableBalanceCurrencies", sdkgo.DefectBranchID, sdkgo.FailureValidation, "profile ID must be a positive decimal integer")
	}
	path := "/borderless-accounts-configuration/profiles/" + url.PathEscape(input.ProfileID) + "/available-currencies"
	return readWiseJSON(call, operation.client, "listAvailableBalanceCurrencies", path, ListAvailableBalanceCurrenciesBranchListed, false, decodePayinCurrencies)
}

// ListPayinCurrenciesOperation reads the available payin currencies.
type ListPayinCurrenciesOperation struct{ client *Client }

// ListPayinCurrencies binds the Query to this client.
func (client *Client) ListPayinCurrencies() ListPayinCurrenciesOperation {
	return ListPayinCurrenciesOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListPayinCurrenciesOperation) Definition() sdkgo.QueryDefinition {
	return ListPayinCurrenciesDefinition
}

// Invoke lists currencies with bank account details support.
func (operation ListPayinCurrenciesOperation) Invoke(call sdkgo.Call, input ListPayinCurrenciesInput) sdkgo.QueryAttempt[PayinCurrenciesOutput] {
	if !validTransferID(input.ProfileID) {
		return wiseReadBranch[PayinCurrenciesOutput]("listPayinCurrencies", sdkgo.DefectBranchID, sdkgo.FailureValidation, "profile ID must be a positive decimal integer")
	}
	path := "/borderless-accounts-configuration/profiles/" + url.PathEscape(input.ProfileID) + "/payin-currencies"
	return readWiseJSON(call, operation.client, "listPayinCurrencies", path, ListPayinCurrenciesBranchListed, false, decodePayinCurrencies)
}

func decodeBalances(body []byte) (BalancesOutput, error) {
	var wire []struct {
		ID       int64  `json:"id"`
		Currency string `json:"currency"`
		Type     string `json:"type"`
		Amount   struct {
			Value float64 `json:"value"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return BalancesOutput{}, err
	}
	if wire == nil {
		return BalancesOutput{}, fmt.Errorf("balance list missing")
	}
	output := BalancesOutput{Balances: make([]BalanceSummary, 0, len(wire))}
	for _, balance := range wire {
		if balance.ID <= 0 || !validCurrency(balance.Currency) || (balance.Type != "STANDARD" && balance.Type != "SAVINGS") || balance.Amount.Value < 0 {
			return BalancesOutput{}, fmt.Errorf("balance fields invalid")
		}
		output.Balances = append(output.Balances, BalanceSummary{ID: strconv.FormatInt(balance.ID, 10), Currency: balance.Currency, Type: balance.Type, AvailableAmount: balance.Amount.Value})
	}
	return output, nil
}

func decodePayinCurrencies(body []byte) (PayinCurrenciesOutput, error) {
	var currencies []string
	if err := json.Unmarshal(body, &currencies); err != nil {
		return PayinCurrenciesOutput{}, err
	}
	if currencies == nil {
		return PayinCurrenciesOutput{}, fmt.Errorf("currency list missing")
	}
	for _, currency := range currencies {
		if !validCurrency(currency) {
			return PayinCurrenciesOutput{}, fmt.Errorf("invalid currency code")
		}
	}
	return PayinCurrenciesOutput{Currencies: currencies}, nil
}
