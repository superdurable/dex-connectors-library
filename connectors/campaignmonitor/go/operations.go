// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

import (
	"encoding/json"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListClientsInput selects the account's accessible clients without filters.
type ListClientsInput struct{}

// ClientMetadata identifies one Campaign Monitor client without secrets.
type ClientMetadata struct {
	// ID is the provider's client identifier.
	ID string `json:"id"`
	// Name is the client display name.
	Name string `json:"name"`
}

// ClientsList contains all returned client identities.
type ClientsList struct {
	// Clients is the account's accessible client list.
	Clients []ClientMetadata `json:"clients"`
}

// ListClientsOperation retrieves client identities from the account API.
type ListClientsOperation struct{ client *Client }

// ListClients binds the client discovery Query to this client.
func (client *Client) ListClients() ListClientsOperation { return ListClientsOperation{client} }

// Definition returns the manifest-generated client discovery contract.
func (ListClientsOperation) Definition() sdkgo.QueryDefinition { return ListClientsDefinition }

// Invoke reads accessible client IDs and names.
func (operation ListClientsOperation) Invoke(call sdkgo.Call, _ ListClientsInput) sdkgo.QueryAttempt[ClientsList] {
	branches := queryBranches{found: ListClientsBranchListed, rejected: ListClientsBranchProviderRejected, invalid: ListClientsBranchInvalidResponse, defect: ListClientsBranchDefect}
	return invokeQuery(call, operation.client, "listClients", "/clients.json", branches, decodeClients)
}

func decodeClients(body []byte) (ClientsList, error) {
	var records []struct {
		ClientID string `json:"ClientID"`
		Name     string `json:"Name"`
	}
	if err := json.Unmarshal(body, &records); err != nil {
		return ClientsList{}, err
	}
	if records == nil {
		return ClientsList{}, fmt.Errorf("client list missing")
	}
	clients := make([]ClientMetadata, 0, len(records))
	for _, record := range records {
		if record.ClientID == "" || record.Name == "" {
			return ClientsList{}, fmt.Errorf("client identity missing")
		}
		clients = append(clients, ClientMetadata{ID: record.ClientID, Name: record.Name})
	}
	return ClientsList{Clients: clients}, nil
}
