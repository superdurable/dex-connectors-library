// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mockfixture is the hand-written half of the connector mock code generation fixture.
package mockfixture

import (
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }

// Client keeps the configuration that generated constructors pass to New.
type Client struct {
	config Config
}

// GetWidgetInput selects one widget.
type GetWidgetInput struct {
	ID string `json:"id"`
}

// Widget is one fixture widget.
type Widget struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
}

// ListWidgetsInput selects one one-based page; zero selects the first.
type ListWidgetsInput struct {
	Page int `json:"page,omitempty"`
}

// WidgetPage is one page of widgets. NextPage is zero on the last page.
type WidgetPage struct {
	Widgets  []Widget `json:"widgets"`
	NextPage int      `json:"nextPage"`
}

// CreateWidgetInput names the widget to create.
type CreateWidgetInput struct {
	Name string `json:"name"`
}

// New applies manifest defaults and validates config.
func New(config Config, _ CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	client := &Client{config: config}
	for _, option := range options {
		option.applyClientOption(client)
	}
	return client, nil
}

// GetWidget returns the fixture query, which never calls a provider.
func (client *Client) GetWidget() GetWidgetOperation { return GetWidgetOperation{} }

// ListWidgets returns the fixture listing query, which never calls a provider.
func (client *Client) ListWidgets() ListWidgetsOperation { return ListWidgetsOperation{} }

// CreateWidget returns the fixture mutation, which never calls a provider.
func (client *Client) CreateWidget() CreateWidgetOperation { return CreateWidgetOperation{} }

// GetWidgetOperation answers defect because the fixture has no provider.
type GetWidgetOperation struct{}

// Definition returns the generated query definition.
func (GetWidgetOperation) Definition() sdkgo.QueryDefinition { return GetWidgetDefinition }

// Invoke selects the defect branch.
func (GetWidgetOperation) Invoke(sdkgo.Call, GetWidgetInput) sdkgo.QueryAttempt[Widget] {
	return sdkgo.NewQueryBranch(GetWidgetBranchDefect, Widget{}, nil, sdkgo.Receipt{})
}

// ListWidgetsOperation answers defect because the fixture has no provider.
type ListWidgetsOperation struct{}

// Definition returns the generated query definition.
func (ListWidgetsOperation) Definition() sdkgo.QueryDefinition { return ListWidgetsDefinition }

// Invoke selects the defect branch.
func (ListWidgetsOperation) Invoke(sdkgo.Call, ListWidgetsInput) sdkgo.QueryAttempt[WidgetPage] {
	return sdkgo.NewQueryBranch(ListWidgetsBranchDefect, WidgetPage{}, nil, sdkgo.Receipt{})
}

// CreateWidgetOperation answers defect because the fixture has no provider.
type CreateWidgetOperation struct{}

// Definition returns the generated mutation definition.
func (CreateWidgetOperation) Definition() sdkgo.MutationDefinition { return CreateWidgetDefinition }

// IdempotencyKey uses the stable call identity.
func (CreateWidgetOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateWidgetInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke selects the defect branch.
func (CreateWidgetOperation) Invoke(sdkgo.Call, CreateWidgetInput) sdkgo.MutationAttempt[Widget] {
	return sdkgo.NewMutationBranch(CreateWidgetBranchDefect, Widget{}, nil, sdkgo.Receipt{})
}
