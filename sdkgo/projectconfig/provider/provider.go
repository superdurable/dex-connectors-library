// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package provider adapts shared project configuration storage to typed Connector SDK interfaces.
// Dex Web imports only projectconfig, avoiding the Dex Worker SDK's generated protocol registry.
package provider

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// Config binds typed credentials to one trusted project connection and complete codecs.
type Config[C any] struct {
	// Store is the durable shared credential authority.
	Store *projectconfig.ConnectionStore
	// Key identifies the connector and named connection allowed for this provider.
	Key projectconfig.ConnectionKey
	// Decode validates complete private JSON into connector-specific credentials.
	Decode projectconfig.CredentialDecoder[C]
	// Encode returns complete replacement material, preserving omitted renewal tokens.
	Encode projectconfig.CredentialEncoder[C]
	// RefreshTimeout bounds one provider request; zero uses 30 seconds and the maximum is two minutes.
	RefreshTimeout time.Duration
}

// CredentialProvider supports typed SDK resolution with expiry-only refresh coordinated across replicas.
type CredentialProvider[C any] struct {
	resolver *projectconfig.CredentialResolver[C]
	key      projectconfig.ConnectionKey
}

// NewCredentialProvider validates the immutable connector boundary and shared storage configuration without provider calls.
func NewCredentialProvider[C any](config *Config[C]) (*CredentialProvider[C], error) {
	if config == nil {
		return nil, errors.New("project credential provider configuration is required")
	}
	resolver, err := projectconfig.NewCredentialResolver(&projectconfig.CredentialResolverConfig[C]{Store: config.Store, Key: config.Key, Decode: config.Decode, Encode: config.Encode, RefreshTimeout: config.RefreshTimeout})
	if err != nil {
		return nil, err
	}
	return &CredentialProvider[C]{resolver: resolver, key: config.Key}, nil
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

// ResolveWithRefresh performs provider refresh only after known expiry and winning durable admission.
// The legacy driver's RefreshRequired skew does not override this package's on-use policy.
func (provider *CredentialProvider[C]) ResolveWithRefresh(ctx context.Context, call sdkgo.Call, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	var zero C
	if err := provider.validateCall(call); err != nil {
		return zero, err
	}
	if driver == nil {
		return zero, errors.New("credential refresh driver is required")
	}
	adapter := refreshAdapter[C]{driver: driver}
	value, err := provider.resolver.ResolveWithRefresh(ctx, adapter.refresh)
	return value, classifyError(err)
}

// ResolveAfterRejection refreshes only if authoritative expiry has elapsed; an unclassified HTTP 401 cannot force rotation.
func (provider *CredentialProvider[C]) ResolveAfterRejection(ctx context.Context, call sdkgo.Call, driver sdkgo.CredentialRefreshDriver[C]) (C, error) {
	var zero C
	if err := provider.validateCall(call); err != nil {
		return zero, err
	}
	if driver == nil {
		return zero, errors.New("credential refresh driver is required")
	}
	adapter := refreshAdapter[C]{driver: driver}
	value, err := provider.resolver.ResolveAfterRejection(ctx, adapter.refresh)
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

func (adapter refreshAdapter[C]) refresh(ctx context.Context, state projectconfig.RefreshState[C]) (projectconfig.RefreshResult[C], error) {
	result, err := adapter.driver.Refresh(ctx, sdkgo.CredentialRefreshState[C]{Credentials: state.Credentials, ExpiresAt: state.ExpiresAt, Now: state.Now})
	return projectconfig.RefreshResult[C]{Credentials: result.Credentials, ExpiresAt: result.ExpiresAt}, err
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
