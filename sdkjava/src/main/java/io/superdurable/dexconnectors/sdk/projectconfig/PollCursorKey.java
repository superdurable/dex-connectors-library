// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.util.Objects;

/**
 * One Trigger binding's poll cursor identity: the same four parts as its Trigger inbox key.
 */
public final class PollCursorKey implements PollCursorIdentity {
    private final String connectorId;
    private final String connectionName;
    private final String triggerName;
    private final String bindingName;

    /**
     * Creates a key; {@link #path(Scope)} validates it.
     *
     * @param connectorId the manifest connector ID
     * @param connectionName the application connection the Trigger reads
     * @param triggerName the manifest Trigger name
     * @param bindingName the application binding of the Trigger
     */
    public PollCursorKey(
            final String connectorId, final String connectionName, final String triggerName, final String bindingName) {
        this.connectorId = connectorId == null ? "" : connectorId;
        this.connectionName = connectionName == null ? "" : connectionName;
        this.triggerName = triggerName == null ? "" : triggerName;
        this.bindingName = bindingName == null ? "" : bindingName;
    }

    /**
     * Returns the head key
     * {@code <scope>/poll-cursors/<connector>/<b64url(connection)>/<trigger>/<b64url(binding)>/head}.
     *
     * @param scope the project scope
     * @return the head key
     * @throws IllegalArgumentException if the identity is invalid
     */
    @Override
    public String path(final Scope scope) {
        PollCursorNames.validate(connectorId, triggerName, connectionName, bindingName);
        return scope.prefix() + "/poll-cursors/" + connectorId + "/" + ConnectionKey.base64Url(connectionName) + "/"
                + triggerName + "/" + ConnectionKey.base64Url(bindingName) + "/head";
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
     * Returns the connection name.
     *
     * @return the connection name
     */
    public String getConnectionName() {
        return connectionName;
    }

    /**
     * Returns the Trigger name.
     *
     * @return the Trigger name
     */
    public String getTriggerName() {
        return triggerName;
    }

    /**
     * Returns the binding name.
     *
     * @return the binding name
     */
    public String getBindingName() {
        return bindingName;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof PollCursorKey)) {
            return false;
        }
        final PollCursorKey that = (PollCursorKey) other;
        return connectorId.equals(that.connectorId) && connectionName.equals(that.connectionName)
                && triggerName.equals(that.triggerName) && bindingName.equals(that.bindingName);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(connectorId, connectionName, triggerName, bindingName);
    }
}
