// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

type forbiddenStore struct{}

func (forbiddenStore) ReadObject(context.Context, string, string) (projectconfig.Object, error) {
	panic("wrong connector call reached storage")
}
func (forbiddenStore) CreateObject(context.Context, string, []byte) (projectconfig.Object, error) {
	panic("wrong connector call wrote storage")
}
func (forbiddenStore) CompareAndSwapObject(context.Context, string, string, []byte) (projectconfig.Object, error) {
	panic("wrong connector call wrote storage")
}
func TestTypedProviderRejectsCrossConnectorBeforeStorage(t *testing.T) {
	store, err := projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: forbiddenStore{}, Scope: projectconfig.Scope{ProjectID: "a7k2", Kind: "live"}})
	require.NoError(t, err)
	provider, err := NewCredentialProvider(&Config[string]{Store: store, Key: projectconfig.ConnectionKey{ConnectorID: "example", ConnectionName: "primary"}, Decode: func(json.RawMessage) (string, error) { return "", nil }, Encode: func(string) (json.RawMessage, error) { return nil, errors.New("unused") }})
	require.NoError(t, err)
	var _ sdkgo.CredentialProvider[string] = provider
	var _ sdkgo.ContextCredentialProvider[string] = provider
	var _ sdkgo.RefreshingCredentialProvider[string] = provider
	var _ sdkgo.RejectedCredentialRefreshingProvider[string] = provider
	for _, call := range []sdkgo.Call{{Operation: sdkgo.OperationRef{ConnectorID: "different", OperationID: "readValue"}, Connection: sdkgo.ConnectionRef{Name: "primary"}}, {Operation: sdkgo.OperationRef{ConnectorID: "example", OperationID: "readValue"}, Connection: sdkgo.ConnectionRef{Name: "different"}}} {
		_, err := provider.ResolveContext(context.Background(), call)
		require.Error(t, err)
	}
}
