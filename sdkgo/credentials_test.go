// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

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

func TestResolveCredentialRejectsMissingDependencies(t *testing.T) {
	_, err := sdkgo.ResolveCredential[string](context.Background(), nil, sdkgo.Call{}, unusedRefreshDriver{})
	require.ErrorContains(t, err, "credential provider is required")
	reference := sdkgo.ConnectionRef{Provider: "example", Name: "primary"}
	provider := sdkgo.StaticCredentialProvider[string]{reference: "secret"}
	_, err = sdkgo.ResolveCredential(context.Background(), provider, sdkgo.Call{Connection: reference}, nil)
	require.ErrorContains(t, err, "credential refresh driver is required")
}
