// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package multipleauthselection is the hand-written half of the multiple-auth-selection code generation fixture.
package multipleauthselection

import "github.com/superdurable/dex-connectors-library/sdkgo"

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }

// Client keeps the configuration and credential provider that generated constructors pass to New.
type Client struct {
	config      Config
	credentials sdkgo.CredentialProvider[Credentials]
}

// GetThingInput is the fixture operation input.
type GetThingInput struct{}

// GetThingOutput is the fixture operation output.
type GetThingOutput struct{}

// GetThingOperation selects the found branch without a provider call.
type GetThingOperation struct{}

// New applies manifest defaults and validates config.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	client := &Client{config: config, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	return client, nil
}

// GetThing returns the fixture query.
func (client *Client) GetThing() GetThingOperation { return GetThingOperation{} }

// Definition returns the generated query definition.
func (GetThingOperation) Definition() sdkgo.QueryDefinition { return GetThingDefinition }

// Invoke selects the found branch.
func (GetThingOperation) Invoke(sdkgo.Call, GetThingInput) sdkgo.QueryAttempt[GetThingOutput] {
	return sdkgo.NewQueryBranch(GetThingBranchFound, GetThingOutput{}, nil, sdkgo.Receipt{})
}
