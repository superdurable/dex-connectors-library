// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Objects;

/**
 * Ordinary settings and logical credential identity for one named connector connection.
 */
public final class ConnectionConfiguration {
    private final String connectorId;
    private final String connectionName;
    private final String modulePath;
    private final String provider;
    private final ObjectNode configuration;
    private final String authMethodId;
    private final String sharedConnectionId;

    /**
     * Creates a connection entry.
     *
     * @param connectorId the manifest connector ID
     * @param connectionName the application connection name
     * @param modulePath the connector module path
     * @param provider the provider name
     * @param configuration the ordinary connector settings
     * @param authMethodId the selected authorization method, or empty
     * @param sharedConnectionId the broker-owned shared connection ID, or empty
     */
    public ConnectionConfiguration(
            final String connectorId, final String connectionName, final String modulePath, final String provider,
            final ObjectNode configuration, final String authMethodId, final String sharedConnectionId) {
        this.connectorId = connectorId == null ? "" : connectorId;
        this.connectionName = connectionName == null ? "" : connectionName;
        this.modulePath = modulePath == null ? "" : modulePath;
        this.provider = provider == null ? "" : provider;
        this.configuration = configuration == null ? GoCompatibleJson.newObject() : configuration.deepCopy();
        this.authMethodId = authMethodId == null ? "" : authMethodId;
        this.sharedConnectionId = sharedConnectionId == null ? "" : sharedConnectionId;
    }

    /**
     * Creates an ordinary connection entry without an authorization method.
     *
     * @param connectorId the manifest connector ID
     * @param connectionName the application connection name
     * @param modulePath the connector module path
     * @param provider the provider name
     * @param configuration the ordinary connector settings
     */
    public ConnectionConfiguration(
            final String connectorId, final String connectionName, final String modulePath, final String provider,
            final ObjectNode configuration) {
        this(connectorId, connectionName, modulePath, provider, configuration, "", "");
    }

    /**
     * Returns the manifest connector ID.
     *
     * @return the connector ID
     */
    public String getConnectorId() {
        return connectorId;
    }

    /**
     * Returns the application connection name.
     *
     * @return the connection name
     */
    public String getConnectionName() {
        return connectionName;
    }

    /**
     * Returns the connector module path.
     *
     * @return the module path
     */
    public String getModulePath() {
        return modulePath;
    }

    /**
     * Returns the provider name.
     *
     * @return the provider
     */
    public String getProvider() {
        return provider;
    }

    /**
     * Returns a copy of the ordinary connector settings.
     *
     * @return the settings
     */
    public ObjectNode getConfiguration() {
        return configuration.deepCopy();
    }

    /**
     * Returns the selected authorization method.
     *
     * @return the method ID, or empty
     */
    public String getAuthMethodId() {
        return authMethodId;
    }

    /**
     * Returns the broker-owned shared connection ID.
     *
     * @return the shared connection ID, or empty
     */
    public String getSharedConnectionId() {
        return sharedConnectionId;
    }

    /**
     * Encodes the entry with omitted optional members.
     *
     * @return the JSON object
     */
    public ObjectNode toJson() {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("connectorId", connectorId);
        result.put("connectionName", connectionName);
        result.put("modulePath", modulePath);
        result.put("provider", provider);
        if (!authMethodId.isEmpty()) {
            result.put("authMethodId", authMethodId);
        }
        if (!sharedConnectionId.isEmpty()) {
            result.put("sharedConnectionId", sharedConnectionId);
        }
        result.set("configuration", configuration.deepCopy());
        return result;
    }

    /**
     * Decodes an ordinary or shared connection entry.
     *
     * @param value the JSON value
     * @return the entry
     * @throws IllegalArgumentException if the entry has unknown fields or no configuration object
     */
    public static ConnectionConfiguration fromJson(final JsonNode value) {
        final ObjectNode record = GoCompatibleJson.requireObject(value, "connection");
        ProjectConfigurationRules.requireFields(record, "connection", "connectorId", "connectionName", "modulePath",
                "provider", "configuration", "authMethodId", "sharedConnectionId");
        return new ConnectionConfiguration(ProjectConfigurationRules.text(record, "connectorId"),
                ProjectConfigurationRules.text(record, "connectionName"),
                ProjectConfigurationRules.text(record, "modulePath"), ProjectConfigurationRules.text(record, "provider"),
                GoCompatibleJson.requireObject(record.get("configuration"), "connection configuration"),
                ProjectConfigurationRules.text(record, "authMethodId"),
                ProjectConfigurationRules.text(record, "sharedConnectionId"));
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof ConnectionConfiguration)) {
            return false;
        }
        final ConnectionConfiguration that = (ConnectionConfiguration) other;
        return connectorId.equals(that.connectorId) && connectionName.equals(that.connectionName)
                && modulePath.equals(that.modulePath) && provider.equals(that.provider)
                && configuration.equals(that.configuration) && authMethodId.equals(that.authMethodId)
                && sharedConnectionId.equals(that.sharedConnectionId);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(connectorId, connectionName, modulePath, provider, configuration, authMethodId,
                sharedConnectionId);
    }
}
