// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import java.util.Objects;
import java.util.regex.Pattern;

/**
 * Stable manifest identity of one connector Trigger.
 */
@JsonPropertyOrder({"connectorId", "triggerName"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class TriggerRef {
    private static final Pattern CONNECTOR_ID_PATTERN = Pattern.compile("^[a-z][a-z0-9-]{1,62}$");
    private static final Pattern TRIGGER_NAME_PATTERN = Pattern.compile("^[a-z][A-Za-z0-9]+$");

    private final String connectorId;
    private final String triggerName;

    /**
     * Creates a Trigger reference; call {@link #validate()} before use.
     *
     * @param connectorId the DNS-like manifest connector ID
     * @param triggerName the lower-camel manifest Trigger name
     */
    @JsonCreator
    public TriggerRef(
            @JsonProperty(value = "connectorId", required = true) final String connectorId,
            @JsonProperty(value = "triggerName", required = true) final String triggerName) {
        this.connectorId = connectorId == null ? "" : connectorId;
        this.triggerName = triggerName == null ? "" : triggerName;
    }

    /**
     * Requires a DNS-like connector ID and a lower-camel Trigger name.
     *
     * @throws IllegalArgumentException if either identity is malformed
     */
    public void validate() {
        if (!CONNECTOR_ID_PATTERN.matcher(connectorId).matches()) {
            throw new IllegalArgumentException("connector ID must be DNS-like and 2-63 characters");
        }
        if (!TRIGGER_NAME_PATTERN.matcher(triggerName).matches()) {
            throw new IllegalArgumentException("trigger name must be lower camel case");
        }
    }

    /**
     * Returns the DNS-like manifest connector ID.
     *
     * @return the connector ID
     */
    @JsonProperty("connectorId")
    public String getConnectorId() {
        return connectorId;
    }

    /**
     * Returns the lower-camel manifest Trigger name.
     *
     * @return the Trigger name
     */
    @JsonProperty("triggerName")
    public String getTriggerName() {
        return triggerName;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof TriggerRef)) {
            return false;
        }
        final TriggerRef that = (TriggerRef) other;
        return connectorId.equals(that.connectorId) && triggerName.equals(that.triggerName);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(connectorId, triggerName);
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return connectorId + " " + triggerName;
    }
}
