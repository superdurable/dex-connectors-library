// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type contextCredentialProvider struct {
	called bool
}

func (*contextCredentialProvider) Resolve(sdkgo.Call) (string, error) {
	return "unexpected", nil
}

func (provider *contextCredentialProvider) ResolveContext(ctx context.Context, _ sdkgo.Call) (string, error) {
	provider.called = true
	return "", ctx.Err()
}

type rejectionRefreshingProvider struct {
	called bool
}

func (*rejectionRefreshingProvider) Resolve(sdkgo.Call) (string, error) {
	return "stale", nil
}

func (provider *rejectionRefreshingProvider) ResolveAfterRejection(
	ctx context.Context,
	_ sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[string],
) (string, error) {
	provider.called = true
	return "refreshed", ctx.Err()
}

type unusedRefreshDriver struct{}

func (unusedRefreshDriver) RefreshRequired(sdkgo.CredentialRefreshState[string]) bool { return true }

func (unusedRefreshDriver) Refresh(
	context.Context,
	sdkgo.CredentialRefreshState[string],
) (sdkgo.CredentialRefreshResult[string], error) {
	return sdkgo.CredentialRefreshResult[string]{Credentials: "unexpected", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestResolveCredentialPreservesStaticProviderBehavior(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "example", Name: "primary"}
	provider := sdkgo.StaticCredentialProvider[string]{reference: "secret"}
	resolved, err := sdkgo.ResolveCredential(
		context.Background(), provider, sdkgo.Call{Connection: reference}, unusedRefreshDriver{},
	)
	require.NoError(t, err)
	require.Equal(t, "secret", resolved)
}

func TestResolveCredentialUsesContextProviderOutsideDexStep(t *testing.T) {
	provider := &contextCredentialProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resolved, err := sdkgo.ResolveCredential(ctx, provider, sdkgo.Call{}, unusedRefreshDriver{})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, resolved)
	require.True(t, provider.called)
}

func TestResolveCredentialRejectsMissingDependencies(t *testing.T) {
	_, err := sdkgo.ResolveCredential[string](context.Background(), nil, sdkgo.Call{}, unusedRefreshDriver{})
	require.ErrorContains(t, err, "credential provider is required")
	reference := sdkgo.ConnectionRef{Provider: "example", Name: "primary"}
	provider := sdkgo.StaticCredentialProvider[string]{reference: "secret"}
	_, err = sdkgo.ResolveCredential(context.Background(), provider, sdkgo.Call{Connection: reference}, nil)
	require.ErrorContains(t, err, "credential refresh driver is required")
}

func TestResolveCredentialAfterRejectionUsesForcedRefreshProvider(t *testing.T) {
	provider := &rejectionRefreshingProvider{}
	credentials, err := sdkgo.ResolveCredentialAfterRejection(
		context.Background(), provider, sdkgo.Call{}, unusedRefreshDriver{},
	)
	require.NoError(t, err)
	require.Equal(t, "refreshed", credentials)
	require.True(t, provider.called)
}

func TestResolveCredentialAfterRejectionRejectsUnsupportedProvider(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "example", Name: "primary"}
	provider := sdkgo.StaticCredentialProvider[string]{reference: "secret"}
	_, err := sdkgo.ResolveCredentialAfterRejection(
		context.Background(), provider, sdkgo.Call{Connection: reference}, unusedRefreshDriver{},
	)
	require.ErrorContains(t, err, "does not support refresh after rejection")
}

func TestReauthorizationRequiredErrorPreservesClassification(t *testing.T) {
	cause := errors.New("provider grant was revoked")
	err := sdkgo.NewReauthorizationRequiredError(cause)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.ErrorIs(t, err, cause)
	require.NotContains(t, err.Error(), cause.Error())
}
