// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.node.ObjectNode;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.Objects;

/**
 * Manifest connector ID and application-declared connection name of one project connection.
 */
public final class ConnectionKey {
    private final String connectorId;
    private final String connectionName;

    /**
     * Creates a connection key; {@link #path(Scope)} validates it.
     *
     * @param connectorId the manifest connector ID
     * @param connectionName the application connection name
     */
    public ConnectionKey(final String connectorId, final String connectionName) {
        this.connectorId = connectorId == null ? "" : connectorId;
        this.connectionName = connectionName == null ? "" : connectionName;
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
     * Encodes the stable connection key.
     *
     * @return the JSON object
     */
    public ObjectNode toJson() {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("connectorId", connectorId);
        result.put("connectionName", connectionName);
        return result;
    }

    /**
     * Returns the scoped connection prefix after validating the key.
     *
     * <p>The connection name is unpadded base64url of its UTF-8 bytes, as Go writes it.
     *
     * @param scope the project scope
     * @return {@code <scope>/connections/<connectorId>/<b64url(connectionName)>}
     * @throws IllegalArgumentException if the key is invalid
     */
    public String path(final Scope scope) {
        if (!ProjectConfigurationRules.CONNECTOR_ID.matcher(connectorId).matches() || connectionName.isEmpty()
                || !connectionName.trim().equals(connectionName) || connectionName.length() > 256
                || connectionName.indexOf('\u0000') >= 0 || connectionName.indexOf('\r') >= 0
                || connectionName.indexOf('\n') >= 0) {
            throw new IllegalArgumentException("invalid project connection key");
        }
        return scope.prefix() + "/connections/" + connectorId + "/" + base64Url(connectionName);
    }

    static String base64Url(final String value) {
        return Base64.getUrlEncoder().withoutPadding()
                .encodeToString(GoCompatibleJson.wellFormed(value).getBytes(StandardCharsets.UTF_8));
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof ConnectionKey)) {
            return false;
        }
        final ConnectionKey that = (ConnectionKey) other;
        return connectorId.equals(that.connectorId) && connectionName.equals(that.connectionName);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(connectorId, connectionName);
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return connectorId + "/" + connectionName;
    }
}
