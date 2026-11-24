// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * A pinned ordinary configuration snapshot with independently resolved current credentials.
 *
 * <p>Load one with {@link ProjectConfigurationLoader}. Credentials are read at call time and never
 * copied into Flow state.
 */
public final class LoadedProject {
    private final Configuration configuration;
    private final SnapshotRef snapshot;
    private final ConnectionStore connections;
    private final ConfigurationStore configurations;
    private final CredentialBrokerClient broker;
    private final Map<ConnectionKey, SharedConnection> sharedConnections;

    LoadedProject(
            final Configuration configuration, final SnapshotRef snapshot, final ConnectionStore connections,
            final ConfigurationStore configurations, final CredentialBrokerClient broker,
            final Map<ConnectionKey, SharedConnection> sharedConnections) {
        this.configuration = configuration;
        this.snapshot = snapshot;
        this.connections = connections;
        this.configurations = configurations;
        this.broker = broker;
        this.sharedConnections = Collections.unmodifiableMap(new HashMap<>(sharedConnections));
    }

    /**
     * Returns the pinned configuration.
     *
     * @return the configuration
     */
    public Configuration getConfiguration() {
        return configuration;
    }

    /**
     * Returns the pinned snapshot reference.
     *
     * @return the snapshot
     */
    public SnapshotRef getSnapshot() {
        return snapshot;
    }

    /**
     * Returns the connection store that providers read credentials from.
     *
     * @return the connection store
     */
    public ConnectionStore getConnections() {
        return connections;
    }

    /**
     * Returns the configuration store.
     *
     * @return the configuration store
     */
    public ConfigurationStore getConfigurations() {
        return configurations;
    }

    /**
     * Returns the credential broker client.
     *
     * @return the broker, or {@code null} when the project declares none
     */
    public CredentialBrokerClient getBroker() {
        return broker;
    }

    /**
     * Resolves private material at call time without copying it into a Flow.
     *
     * @param key the connection key
     * @param callId the Call ID that receives the credential, or empty
     * @return a copy of the credential object
     * @throws IOException if storage or the broker fails
     */
    public ObjectNode resolveCredential(final ConnectionKey key, final String callId) throws IOException {
        final SharedConnection shared = sharedConnections.get(key);
        if (shared != null) {
            return shared.resolve(callId);
        }
        return connections.readCredentialMaterial(key).getMaterial().getCredentials();
    }

    /**
     * Asks the broker for a newer revision after a shared credential rejection.
     *
     * @param key the connection key
     * @param callId the Call ID whose credential the provider rejected
     * @return a copy of the newer credential object
     * @throws IOException if the broker fails
     * @throws IllegalArgumentException if the connection is not shared
     */
    public ObjectNode resolveAfterRejection(final ConnectionKey key, final String callId) throws IOException {
        final SharedConnection shared = sharedConnections.get(key);
        if (shared == null) {
            throw new IllegalArgumentException("connection does not use a shared credential broker");
        }
        return shared.resolveAfterRejection(callId);
    }

    /**
     * Loads the application environment, reading only pinned secret versions.
     *
     * @return the environment values by name
     * @throws IOException if a secret cannot be read
     */
    public Map<String, String> resolveApplicationEnvironment() throws IOException {
        final Map<String, String> result = new LinkedHashMap<>();
        for (Map.Entry<String, EnvironmentValue> entry : configuration.getEnvironment().entrySet()) {
            if (entry.getValue().getValue() != null) {
                result.put(entry.getKey(), entry.getValue().getValue());
            } else {
                result.put(entry.getKey(),
                        configurations.readSecretValue(entry.getValue().getSecretRef(), entry.getKey()));
            }
        }
        return result;
    }

    /**
     * Binds one Trigger binding's or feed's durable poll cursor in this project's storage.
     *
     * <p>Hosted projects keep the cursor in project storage.
     *
     * @param identity the cursor identity
     * @return the store, bound without I/O
     * @throws IllegalArgumentException for an invalid identity
     */
    public PollCursorStore pollCursorStore(final PollCursorIdentity identity) {
        return new PollCursorStore(connections.getObjects(), configuration.getScope(), identity);
    }

    /**
     * Binds one Trigger binding's durable inbox in this project's storage.
     *
     * @param key the binding identity
     * @return the inbox, bound without I/O
     * @throws IllegalArgumentException for an invalid identity
     */
    public TriggerInbox triggerInbox(final TriggerInboxKey key) {
        return new TriggerInbox(connections.getObjects(), configuration.getScope(), key);
    }

    /**
     * Returns the shared connection ID of a configured connection.
     *
     * @param key the connection key
     * @return the shared connection ID, or {@code null} when the connection is not shared
     */
    public String sharedConnectionId(final ConnectionKey key) {
        for (ConnectionConfiguration entry : configuration.getConnections()) {
            if (entry.getConnectorId().equals(key.getConnectorId())
                    && entry.getConnectionName().equals(key.getConnectionName())) {
                return entry.getSharedConnectionId().isEmpty() ? null : entry.getSharedConnectionId();
            }
        }
        return null;
    }

    /**
     * Asks the credential broker to relay a shared connection's provider events to this application.
     *
     * @param key the connection key
     * @param registrationPath the absolute path on the broker endpoint's origin
     * @throws IOException if the broker fails
     * @throws IllegalArgumentException without a broker or a shared connection entry
     */
    public void registerRelayedTrigger(final ConnectionKey key, final String registrationPath) throws IOException {
        if (broker == null) {
            throw new IllegalArgumentException(
                    "relayed Trigger registration requires a credential broker in the project configuration");
        }
        final String sharedConnectionId = sharedConnectionId(key);
        if (sharedConnectionId == null) {
            throw new IllegalArgumentException("relayed Trigger registration requires a shared connection entry for "
                    + key.getConnectorId() + "/" + key.getConnectionName());
        }
        broker.registerRelayedTrigger(sharedConnectionId, key.getConnectionName(), registrationPath);
    }
}
