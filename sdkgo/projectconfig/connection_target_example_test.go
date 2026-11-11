// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

type exampleCredentials struct {
	APIKey string `json:"apiKey"`
}

// writeExampleConnections writes a Dex Web connections file whose record has authorizationID and apiKey.
func writeExampleConnections(path, authorizationID, apiKey string) error {
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
		"connections": []any{map[string]any{
			"connectorId": "example", "connectionName": "main", "provider": "example", "authMethodId": "apiKey",
			"modulePath": "github.com/superdurable/dex-connectors-library/connectors/example/go", "authorizationId": authorizationID,
			"configuration": map[string]any{"workspace": "acme"}, "credentials": map[string]any{"apiKey": apiKey},
		}},
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0o600)
}

func ExampleContextWithExpectedConnectionTarget() {
	directory, err := os.MkdirTemp("", "connection-target-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(directory) }()
	path := filepath.Join(directory, "connections.json")
	if err = writeExampleConnections(path, "6f1d3c1e-2b0a-4c55-9d43-0a8f6c2e7b11", "key-for-account-a"); err != nil {
		fmt.Println(err)
		return
	}

	ctx := context.Background()
	project, err := projectconfig.LoadDexWebConnectionsFile(ctx, path)
	if err != nil {
		fmt.Println(err)
		return
	}
	key := projectconfig.ConnectionKey{ConnectorID: "example", ConnectionName: "main"}
	// Record the target that the call was checked against; the fingerprint is safe to persist.
	target, err := project.Configuration.Connections[0].ConnectionTargetFingerprint()
	if err != nil {
		fmt.Println(err)
		return
	}
	resolver, err := projectconfig.NewCredentialResolver(project.Connections, key, func(contents json.RawMessage) (exampleCredentials, error) {
		var credentials exampleCredentials
		return credentials, projectconfig.DecodeCredentials(contents, &credentials)
	}, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	attemptContext := projectconfig.ContextWithExpectedConnectionTarget(ctx, key, target)

	credentials, err := resolver.Resolve(attemptContext)
	fmt.Println(credentials.APIKey, err)

	// Dex Web rebinds the connection to another account before the next attempt resolves its credential.
	if err = writeExampleConnections(path, "c2a9e7d4-8f3b-4e61-a2d5-5b7c9e0f1a23", "key-for-account-b"); err != nil {
		fmt.Println(err)
		return
	}
	_, err = resolver.Resolve(attemptContext)
	fmt.Println(errors.Is(err, projectconfig.ErrConnectionTargetChanged))
	// Output:
	// key-for-account-a <nil>
	// true
}
