// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.Objects;

/**
 * One application binding of a connector Trigger, which owns one project Trigger inbox.
 */
public final class TriggerInboxKey {
    private final String connectorId;
    private final String connectionName;
    private final String triggerName;
    private final String bindingName;

    /**
     * Creates a key; call {@link #validate()} or {@link #path(Scope)} to check it.
     *
     * @param connectorId the manifest connector ID
     * @param connectionName the application-declared connection the Trigger reads
     * @param triggerName the manifest Trigger name
     * @param bindingName the application binding of that Trigger
     */
    public TriggerInboxKey(
            final String connectorId, final String connectionName, final String triggerName, final String bindingName) {
        this.connectorId = connectorId == null ? "" : connectorId;
        this.connectionName = connectionName == null ? "" : connectionName;
        this.triggerName = triggerName == null ? "" : triggerName;
        this.bindingName = bindingName == null ? "" : bindingName;
    }

    /**
     * Requires Go-valid identities: names of at most 256 UTF-8 bytes without edge spaces or line
     * breaks.
     *
     * @throws IllegalArgumentException if an identity is invalid
     */
    public void validate() {
        try {
            PollCursorNames.validate(connectorId, triggerName, connectionName, bindingName);
        } catch (IllegalArgumentException error) {
            throw new IllegalArgumentException("trigger inbox identity is invalid", error);
        }
    }

    /**
     * Returns the inbox head object key in scope, as Go and TypeScript write it.
     *
     * @param scope the project scope
     * @return the head object key
     * @throws IllegalArgumentException if an identity is invalid
     */
    public String path(final Scope scope) {
        validate();
        return scope.prefix() + "/trigger-inboxes/" + connectorId + "/" + base64Url(connectionName) + "/"
                + triggerName + "/" + base64Url(bindingName) + "/head";
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
     * Returns the manifest Trigger name.
     *
     * @return the Trigger name
     */
    public String getTriggerName() {
        return triggerName;
    }

    /**
     * Returns the application binding name.
     *
     * @return the binding name
     */
    public String getBindingName() {
        return bindingName;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof TriggerInboxKey)) {
            return false;
        }
        final TriggerInboxKey that = (TriggerInboxKey) other;
        return connectorId.equals(that.connectorId) && connectionName.equals(that.connectionName)
                && triggerName.equals(that.triggerName) && bindingName.equals(that.bindingName);
    }

    @Override
    public int hashCode() {
        return Objects.hash(connectorId, connectionName, triggerName, bindingName);
    }

    @Override
    public String toString() {
        return connectorId + "/" + connectionName + "/" + triggerName + "/" + bindingName;
    }

    private static String base64Url(final String value) {
        return Base64.getUrlEncoder().withoutPadding().encodeToString(value.getBytes(StandardCharsets.UTF_8));
    }
}
