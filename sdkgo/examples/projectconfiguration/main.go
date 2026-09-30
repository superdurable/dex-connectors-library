// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command projectconfiguration validates a pinned project configuration and optionally reads one typed fixture credential.
// Configure the ordinary snapshot and credentials through Dex Web before running this example.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

type credentials struct{ token sdkgo.SecretString }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	connectorID := flag.String("connector", "", "optional fixture connector ID; no credential is read when omitted")
	connectionName := flag.String("connection", "primary", "logical named connection for the fixture")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	loaded, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	applicationEnvironment, err := loaded.ResolveApplicationEnvironment(ctx)
	if err != nil {
		return err
	}
	if err = applicationEnvironment.Apply(); err != nil {
		return err
	}
	fmt.Printf("Accepted configuration revision %d with %d connections (%s).\n", loaded.Configuration.Revision, len(loaded.Configuration.Connections), loaded.Snapshot.Digest)
	if *connectorID == "" {
		return nil
	}
	credentialProvider, err := provider.NewCredentialProvider(&provider.Config[credentials]{Store: loaded.Connections, Key: projectconfig.ConnectionKey{ConnectorID: *connectorID, ConnectionName: *connectionName}, Decode: decodeCredentials, Encode: encodeCredentials})
	if err != nil {
		return err
	}
	call := sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "fixture", Name: *connectionName}, Operation: sdkgo.OperationRef{ConnectorID: *connectorID, OperationID: "readValue"}}
	resolved, err := credentialProvider.ResolveContext(ctx, call)
	if err != nil {
		return err
	}
	if resolved.token.Reveal() == "" {
		return errors.New("fixture token is absent")
	}
	fmt.Println("Typed credential resolved at the provider boundary; its contents were not printed.")
	return nil
}
func decodeCredentials(contents json.RawMessage) (credentials, error) {
	var value struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(contents, &value); err != nil || value.Token == "" {
		return credentials{}, errors.New("fixture token is unavailable")
	}
	return credentials{token: sdkgo.NewSecretString(value.Token)}, nil
}
func encodeCredentials(value credentials) (json.RawMessage, error) {
	return json.Marshal(struct {
		Token string `json:"token"`
	}{Token: value.token.Reveal()})
}
