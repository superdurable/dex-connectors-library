// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListCustomerSourcesInput requests the sources visible to this Copper user.
type ListCustomerSourcesInput struct{}

// CustomerSourceMetadata identifies one configured lead or opportunity source.
type CustomerSourceMetadata struct {
	// ID is Copper's stable numeric source identifier.
	ID int64 `json:"id"`
	// Name is the source label shown in Copper.
	Name string `json:"name"`
}

// CustomerSourcesList contains the visible source identifiers and names.
type CustomerSourcesList struct {
	// Sources contains only IDs and labels.
	Sources []CustomerSourceMetadata `json:"sources"`
}

// ListCustomerSourcesOperation reads the configured sources.
type ListCustomerSourcesOperation struct{ client *Client }

// ListCustomerSources binds the Query to this client.
func (client *Client) ListCustomerSources() ListCustomerSourcesOperation {
	return ListCustomerSourcesOperation{client}
}

// Definition returns the generated Query contract.
func (ListCustomerSourcesOperation) Definition() sdkgo.QueryDefinition {
	return ListCustomerSourcesDefinition
}

// Invoke reads source IDs and labels visible to the connected user.
func (operation ListCustomerSourcesOperation) Invoke(call sdkgo.Call, _ ListCustomerSourcesInput) sdkgo.QueryAttempt[CustomerSourcesList] {
	branches := queryBranches{found: ListCustomerSourcesBranchListed, rejected: ListCustomerSourcesBranchProviderRejected, invalid: ListCustomerSourcesBranchInvalidResponse, defect: ListCustomerSourcesBranchDefect}
	return invokeQuery(call, operation.client, "listCustomerSources", "/customer_sources", branches, decodeCustomerSources)
}

func decodeCustomerSources(body []byte) (CustomerSourcesList, error) {
	var records []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &records); err != nil {
		return CustomerSourcesList{}, err
	}
	if records == nil {
		return CustomerSourcesList{}, errors.New("customer source list missing")
	}
	sources := make([]CustomerSourceMetadata, 0, len(records))
	for _, record := range records {
		if record.ID <= 0 || strings.TrimSpace(record.Name) == "" {
			return CustomerSourcesList{}, errors.New("customer source identity missing")
		}
		sources = append(sources, CustomerSourceMetadata{ID: record.ID, Name: record.Name})
	}
	return CustomerSourcesList{Sources: sources}, nil
}
