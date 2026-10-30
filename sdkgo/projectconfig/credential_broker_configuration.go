// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"context"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// credentialBrokerTokenName names the scope token's object; the reserved prefix keeps applications from declaring it.
const credentialBrokerTokenName = "DEX_CREDENTIAL_BROKER_TOKEN"

const maximumCredentialBrokerEndpointLength = 2048

var sharedConnectionIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// CreateCredentialBrokerToken writes the scope token that authorizes a scope's credential reads as one immutable private
// object and returns the reference for CredentialBrokerConfiguration.TokenSecretRef. Like CreateApplicationSecret,
// an unknown write is reconciled against the same object key, and the object stays scope-owned.
// The token must never enter a Flow, a log, or a browser response.
func (store *ConfigurationStore) CreateCredentialBrokerToken(ctx context.Context, token string) (ApplicationSecretRef, error) {
	if token == "" || !validEnvironmentValue(token) {
		return ApplicationSecretRef{}, errors.New("invalid credential broker token")
	}
	return store.createSecretObject(ctx, credentialBrokerTokenName, token)
}

// readCredentialBrokerToken reads the exact scope token that the configuration pins.
func (store *ConfigurationStore) readCredentialBrokerToken(ctx context.Context, reference ApplicationSecretRef) (string, error) {
	return store.readSecretValue(ctx, reference, credentialBrokerTokenName)
}

func isSupportedSchemaVersion(version string) bool {
	return version == ConfigurationSchemaVersion || version == previousConfigurationSchemaVersion
}

// schemaVersionFor returns the earliest schema version that can express the document.
func schemaVersionFor(configuration Configuration) string {
	if configuration.CredentialBroker != nil {
		return ConfigurationSchemaVersion
	}
	for _, connection := range configuration.Connections {
		if connection.SharedConnectionID != "" {
			return ConfigurationSchemaVersion
		}
	}
	return previousConfigurationSchemaVersion
}

func validateSharedConnections(configuration Configuration) error {
	hasSharedConnection := false
	for _, connection := range configuration.Connections {
		if connection.SharedConnectionID == "" {
			continue
		}
		if !sharedConnectionIdentity.MatchString(connection.SharedConnectionID) {
			return errors.New("configuration shared connection ID is invalid")
		}
		hasSharedConnection = true
	}
	broker := configuration.CredentialBroker
	if configuration.SchemaVersion == previousConfigurationSchemaVersion && (broker != nil || hasSharedConnection) {
		return errors.New("configuration uses shared connections but declares an earlier schema version")
	}
	if broker == nil {
		if hasSharedConnection {
			return errors.New("configuration shared connections require a credential broker")
		}
		return nil
	}
	return validateCredentialBroker(configuration.Scope, *broker)
}

func validateCredentialBroker(scope Scope, broker CredentialBrokerConfiguration) error {
	if err := validateCredentialBrokerEndpoint(broker.Endpoint); err != nil {
		return err
	}
	prefix, err := scope.Prefix()
	if err != nil {
		return err
	}
	reference := broker.TokenSecretRef
	tokenPrefix := applicationSecretPrefix(prefix, credentialBrokerTokenName)
	if !strings.HasPrefix(reference.Key, tokenPrefix) || !applicationSecretID.MatchString(strings.TrimPrefix(reference.Key, tokenPrefix)) || reference.Version == "" || reference.Version == "null" || strings.ContainsAny(reference.Version, "\x00\r\n") || !applicationSecretDigest.MatchString(reference.Digest) {
		return errors.New("credential broker token reference differs from scope")
	}
	return nil
}

func validateCredentialBrokerEndpoint(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || len(value) > maximumCredentialBrokerEndpointLength || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return errors.New("credential broker endpoint must be an absolute http or https URL")
	}
	if parsed.Scheme == "http" && !isPrivateBrokerHost(parsed.Hostname()) {
		return errors.New("credential broker endpoint may use http only for a loopback, private, single-label or cluster-local host")
	}
	return nil
}

// isPrivateBrokerHost reports hosts that only a trusted network can resolve, where plain http is acceptable.
func isPrivateBrokerHost(hostname string) bool {
	if address := net.ParseIP(hostname); address != nil {
		return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast()
	}
	return hostname == "localhost" || hostname == "host.docker.internal" || !strings.Contains(hostname, ".") ||
		strings.HasSuffix(hostname, ".svc") || strings.HasSuffix(hostname, ".svc.cluster.local")
}
