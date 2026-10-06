// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"encoding/json"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// queryBranches names an operation's branches for the shared read outcome mapping.
type queryBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// queryResponseWire is the QueryResponse of GET /query; the entity list is decoded by the caller.
type queryResponseWire struct {
	QueryResponse *map[string]json.RawMessage `json:"QueryResponse"`
}

// queryAttemptForExchange maps every non-success outcome; requested is the value those branches carry.
func queryAttemptForExchange[OUT any](result quickbooksExchange, requested OUT, receipt sdkgo.Receipt, branches queryBranches) (sdkgo.QueryAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[OUT]{}, false
	case exchangeRetry:
		return sdkgo.NewQueryRetry[OUT](result.failure, result.retryAfter), true
	case exchangeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewQueryBranch(branches.notFound, requested, &result.failure, receipt), true
		}
		return sdkgo.NewQueryBranch(branches.providerRejected, requested, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(branches.invalidResponse, requested, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewQueryBranch(branches.defect, requested, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(branches.providerRejected, requested, &result.failure, receipt), true
	}
}

// mutationBranches names an operation's branches for the shared write outcome mapping.
type mutationBranches struct {
	nameConflict     sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// mutationAttemptForExchange retries what the requestid replays and reports an unusable accepted answer as uncertain.
func mutationAttemptForExchange[OUT any](result quickbooksExchange, requested OUT, receipt sdkgo.Receipt, branches mutationBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRetry:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeInvalid:
		return sdkgo.NewMutationUncertain(requested, result.failure, receipt), true
	case exchangeNameConflict:
		if branches.nameConflict != "" {
			return sdkgo.NewMutationBranch(branches.nameConflict, requested, &result.failure, receipt), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, requested, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(branches.defect, requested, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, requested, &result.failure, receipt), true
	}
}

// decodeEntity reads the one entity of a QuickBooks response such as {"Invoice":{...},"time":"..."}.
func decodeEntity[W any](body []byte, entityName string) (W, error) {
	var zero W
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return zero, errResponseNotJSONObject
	}
	contents, isPresent := envelope[entityName]
	if !isPresent {
		return zero, fmt.Errorf("response has no %s", entityName)
	}
	var entity W
	if err := json.Unmarshal(contents, &entity); err != nil {
		return zero, fmt.Errorf("response %s is malformed", entityName)
	}
	return entity, nil
}

// decodeQueryEntities reads the entity list of a query response; QuickBooks omits it when nothing matches.
func decodeQueryEntities[W any](body []byte, entityName string) ([]W, error) {
	var wire queryResponseWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, errResponseNotJSONObject
	}
	if wire.QueryResponse == nil {
		return nil, fmt.Errorf("response has no QueryResponse")
	}
	contents, isPresent := (*wire.QueryResponse)[entityName]
	if !isPresent {
		return []W{}, nil
	}
	var entities []W
	if err := json.Unmarshal(contents, &entities); err != nil {
		return nil, fmt.Errorf("QueryResponse %s is not a list", entityName)
	}
	return entities, nil
}
