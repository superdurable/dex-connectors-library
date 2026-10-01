// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"encoding/json"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// helpScoutProvider is the manifest's spec.provider, which names the connection's provider.
const helpScoutProvider = "helpscout"

// NewLocalRenewingConnection loads the named connection from the local connection file that Dex Web
// writes, like the generated NewLocalConnection, but resolves credentials through
// localconfig.NewRefreshingCredentialProvider. The connector then obtains an access token from the App ID
// and App Secret, stores it with its expiry in the file under a process-local lock, and replaces it five
// minutes before expiry or after a 401. Use it for every local Help Scout connection: the generated
// NewLocalConnection, and NewLocalConversationEventTrigger built on it, use a provider that cannot store
// the token, so their Help Scout calls return Retry until the Step fails. Configuration is captured at
// startup; credentials are reread before every call.
func NewLocalRenewingConnection(store *localconfig.Store, connectionName string, options ...Option) (Connection, error) {
	if store == nil {
		return Connection{}, fmt.Errorf("local connector configuration store is required")
	}
	reference := sdkgo.ConnectionRef{Provider: helpScoutProvider, Name: connectionName}
	if err := reference.Validate(); err != nil {
		return Connection{}, fmt.Errorf("helpscout local connection: %w", err)
	}
	var config Config
	if err := store.DecodeConfiguration(ConnectorID, connectionName, &config); err != nil {
		return Connection{}, err
	}
	credentials := localconfig.NewRefreshingCredentialProvider(store, ConnectorID, connectionName, decodeLocalCredentials, encodeRenewableCredentials)
	client, err := New(config, credentials, options...)
	if err != nil {
		return Connection{}, err
	}
	return NewConnection(client, reference)
}

// encodeRenewableCredentials writes the same credentials object decodeLocalCredentials reads.
func encodeRenewableCredentials(credentials Credentials) (json.RawMessage, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		AppID         string `json:"app_id"`
		AppSecret     string `json:"app_secret"`
		AccessToken   string `json:"access_token,omitempty"`
		WebhookSecret string `json:"webhook_secret,omitempty"`
	}{
		AppID: credentials.AppID, AppSecret: credentials.AppSecret.Reveal(),
		AccessToken: credentials.AccessToken.Reveal(), WebhookSecret: credentials.WebhookSecret.Reveal(),
	})
}
