// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package provider adapts shared project configuration storage to typed Connector SDK credential interfaces.
// Generated connector code is its only caller: applications open a connection with the connector's
// NewProjectConnection. Dex Web imports only projectconfig, avoiding the Dex Worker SDK's protocol registry.
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// CredentialProvider resolves credentials that a connector never refreshes, such as API keys.
type CredentialProvider[C any] struct {
	resolver *projectconfig.CredentialResolver[C]
	key      projectconfig.ConnectionKey
}

// RefreshingCredentialProvider also refreshes expired credentials and persists the replacement once across replicas.
type RefreshingCredentialProvider[C any] struct {
	CredentialProvider[C]
}

// NewCredentialProvider binds credentials that are never refreshed to one project connection.
func NewCredentialProvider[C any](store *projectconfig.ConnectionStore, key projectconfig.ConnectionKey, decode projectconfig.CredentialDecoder[C]) (*CredentialProvider[C], error) {
	resolver, err := projectconfig.NewCredentialResolver(store, key, decode, nil)
	if err != nil {
		return nil, err
	}
	return &CredentialProvider[C]{resolver: resolver, key: key}, nil
}

// NewRefreshingCredentialProvider binds renewable credentials to one project connection.
// encode returns the complete replacement material, including renewal tokens a provider omitted.
func NewRefreshingCredentialProvider[C any](store *projectconfig.ConnectionStore, key projectconfig.ConnectionKey, decode projectconfig.CredentialDecoder[C], encode projectconfig.CredentialEncoder[C]) (*RefreshingCredentialProvider[C], error) {
	if encode == nil {
		return nil, errors.New("project credential encoder is required")
	}
	resolver, err := projectconfig.NewCredentialResolver(store, key, decode, encode)
	if err != nil {
		return nil, err
	}
	return &RefreshingCredentialProvider[C]{CredentialProvider: CredentialProvider[C]{resolver: resolver, key: key}}, nil
}

// Resolve uses the active Dex Step context without refreshing credentials.
func (provider *CredentialProvider[C]) Resolve(call sdkgo.Call) (C, error) {
	ctx := context.Background()
	if call.Context != nil {
		ctx = call.Context
	}
	return provider.ResolveContext(ctx, call)
}

// ResolveContext reads current credentials without refreshing; known-expired credentials are rejected.
func (provider *CredentialProvider[C]) ResolveContext(ctx context.Context, call sdkgo.Call) (C, error) {
	var zero C
	if err := provider.validateCall(call); err != nil {
		return zero, err
	}
	value, err := provider.resolver.Resolve(ctx)
	return value, classifyError(err)
}

// ResolveWithRefresh refreshes known-expired credentials and those the driver requires, such as a missing or
// soon-expiring access token, after winning durable admission.
func (provider *RefreshingCredentialProvider[C]) ResolveWithRefresh(ctx context.Context, call sdkgo.Call, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	var zero C
	if err := provider.validateCall(call); err != nil {
		return zero, err
	}
	if driver == nil {
		return zero, errors.New("credential refresh driver is required")
	}
	value, err := provider.resolver.ResolveWithRefresh(ctx, refreshAdapter[C]{driver: driver})
	return value, classifyError(err)
}

// ResolveAfterRejection refreshes only if authoritative expiry has elapsed; an unclassified HTTP 401 cannot force rotation.
func (provider *RefreshingCredentialProvider[C]) ResolveAfterRejection(ctx context.Context, call sdkgo.Call, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	var zero C
	if err := provider.validateCall(call); err != nil {
		return zero, err
	}
	if driver == nil {
		return zero, errors.New("credential refresh driver is required")
	}
	value, err := provider.resolver.ResolveAfterRejection(ctx, refreshAdapter[C]{driver: driver})
	return value, classifyError(err)
}

func (provider *CredentialProvider[C]) validateCall(call sdkgo.Call) error {
	if call.Operation.ConnectorID != provider.key.ConnectorID || call.Connection.Name != provider.key.ConnectionName {
		return errors.New("connector call differs from configured project connection")
	}
	if err := call.Operation.Validate(); err != nil {
		return errors.New("connector operation identity is invalid")
	}
	return nil
}

type refreshAdapter[C any] struct {
	driver sdkgo.CredentialRefreshDriver[C]
}

// RefreshRequired asks the connector's driver, which knows its provider's token rules.
func (adapter refreshAdapter[C]) RefreshRequired(state projectconfig.RefreshState[C]) bool {
	return adapter.driver.RefreshRequired(refreshState(state))
}

// Refresh marks a driver's reauthorization-required failure for projectconfig, which cannot import sdkgo.
func (adapter refreshAdapter[C]) Refresh(ctx context.Context, state projectconfig.RefreshState[C]) (projectconfig.RefreshResult[C], error) {
	result, err := adapter.driver.Refresh(ctx, refreshState(state))
	if sdkgo.IsReauthorizationRequired(err) {
		err = fmt.Errorf("%w: %w", projectconfig.ErrReauthorizationRequired, err)
	}
	return projectconfig.RefreshResult[C]{Credentials: result.Credentials, ExpiresAt: result.ExpiresAt}, err
}

func refreshState[C any](state projectconfig.RefreshState[C]) sdkgo.CredentialRefreshState[C] {
	return sdkgo.CredentialRefreshState[C]{Credentials: state.Credentials, ExpiresAt: state.ExpiresAt, Now: state.Now}
}

func classifyError(err error) error {
	if errors.Is(err, projectconfig.ErrReauthorizationRequired) {
		return sdkgo.ErrReauthorizationRequired
	}
	return err
}

// LoadOperationConfiguration loads typed startup settings from an immutable configuration snapshot.
func LoadOperationConfiguration[T any](configuration projectconfig.Configuration, reference sdkgo.ConnectorConfigurationRef) (sdkgo.ConnectorLoadedConfiguration[T], error) {
	var value T
	if err := reference.Validate(); err != nil {
		return sdkgo.ConnectorLoadedConfiguration[T]{}, err
	}
	if err := configuration.DecodeOperationConfiguration(reference.ConnectorID, reference.ConnectionName, reference.OperationID, reference.FlowType, reference.StepType, &value); err != nil {
		return sdkgo.ConnectorLoadedConfiguration[T]{}, err
	}
	return sdkgo.ConnectorLoadedConfiguration[T]{Reference: reference, Value: value}, nil
}
