// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import java.util.Objects;
import java.util.regex.Pattern;

/**
 * Stable manifest identity of a connector operation or Trigger.
 */
@JsonPropertyOrder({"connectorId", "operationId"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class OperationRef {
    static final Pattern CONNECTOR_ID_PATTERN = Pattern.compile("^[a-z][a-z0-9-]{1,62}$");
    static final Pattern OPERATION_ID_PATTERN = Pattern.compile("^[a-z][A-Za-z0-9]+$");

    private final String connectorId;
    private final String operationId;

    /**
     * Creates an operation reference; call {@link #validate()} before use.
     *
     * @param connectorId the DNS-like connector ID
     * @param operationId the lower-camel operation ID
     */
    @JsonCreator
    public OperationRef(
            @JsonProperty(value = "connectorId", required = true) final String connectorId,
            @JsonProperty(value = "operationId", required = true) final String operationId) {
        this.connectorId = connectorId == null ? "" : connectorId;
        this.operationId = operationId == null ? "" : operationId;
    }

    /**
     * Requires a DNS-like connector ID and a lower-camel operation ID.
     *
     * @throws IllegalArgumentException if either identity is malformed
     */
    public void validate() {
        if (!CONNECTOR_ID_PATTERN.matcher(connectorId).matches()) {
            throw new IllegalArgumentException("connector ID must be DNS-like and 2-63 characters");
        }
        if (!OPERATION_ID_PATTERN.matcher(operationId).matches()) {
            throw new IllegalArgumentException("operation ID must be lower camel case");
        }
    }

    /**
     * Returns the DNS-like connector ID.
     *
     * @return the connector ID
     */
    @JsonProperty("connectorId")
    public String getConnectorId() {
        return connectorId;
    }

    /**
     * Returns the lower-camel operation ID.
     *
     * @return the operation ID
     */
    @JsonProperty("operationId")
    public String getOperationId() {
        return operationId;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof OperationRef)) {
            return false;
        }
        final OperationRef that = (OperationRef) other;
        return connectorId.equals(that.connectorId) && operationId.equals(that.operationId);
    }

    @Override
    public int hashCode() {
        return Objects.hash(connectorId, operationId);
    }

    @Override
    public String toString() {
        return connectorId + " " + operationId;
    }
}
