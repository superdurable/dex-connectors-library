// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * One answered mock call, including replay and retry classification.
 *
 * @param <I> the operation input type
 */
public final class RecordedCall<I> {
    private final String connectorId;
    private final String operationId;
    private final String connectionName;
    private final String flowId;
    private final String callId;
    private final I input;
    private final String branch;
    private final boolean isRetry;
    private final boolean isReplay;
    private final boolean isUnscripted;

    /**
     * Creates a recorded call.
     *
     * @param connectorId the connector ID
     * @param operationId the operation ID
     * @param connectionName the connection name
     * @param flowId the Dex Flow ID
     * @param callId the Call ID
     * @param input the operation input, kept in memory only
     * @param branch the answered branch, or empty for a retry
     * @param isRetry whether the call answered a retry
     * @param isReplay whether the call replayed a terminal keyed result
     * @param isUnscripted whether no script answered the call
     */
    public RecordedCall(
            final String connectorId, final String operationId, final String connectionName, final String flowId,
            final String callId, final I input, final String branch, final boolean isRetry, final boolean isReplay,
            final boolean isUnscripted) {
        this.connectorId = connectorId;
        this.operationId = operationId;
        this.connectionName = connectionName;
        this.flowId = flowId;
        this.callId = callId;
        this.input = input;
        this.branch = branch == null ? "" : branch;
        this.isRetry = isRetry;
        this.isReplay = isReplay;
        this.isUnscripted = isUnscripted;
    }

    /**
     * Encodes the process-level Go {@code RecordedCall} JSON shape.
     *
     * @return the JSON object
     */
    public ObjectNode toJson() {
        final ObjectNode result = JsonNodeFactory.instance.objectNode();
        result.put("connectorId", connectorId);
        result.put("operationId", operationId);
        result.put("connectionName", connectionName);
        result.put("flowId", flowId);
        result.put("callId", callId);
        if (!branch.isEmpty()) {
            result.put("branch", branch);
        }
        if (isRetry) {
            result.put("retry", true);
        }
        if (isReplay) {
            result.put("replay", true);
        }
        if (isUnscripted) {
            result.put("unscripted", true);
        }
        return result;
    }

    /**
     * Encodes the Go connector report row, without private input values, in Go field order.
     *
     * @return the ordered row fields
     */
    public Map<String, Object> toReportRow() {
        final Map<String, Object> result = new LinkedHashMap<>();
        result.put("connection", connectionName);
        result.put("operation", operationId);
        if (!branch.isEmpty()) {
            result.put("branch", branch);
        }
        result.put("connectorId", connectorId);
        if (!flowId.isEmpty()) {
            result.put("flowId", flowId);
        }
        if (!callId.isEmpty()) {
            result.put("callId", callId);
        }
        if (isRetry) {
            result.put("retry", Boolean.TRUE);
        }
        if (isReplay) {
            result.put("replay", Boolean.TRUE);
        }
        if (isUnscripted) {
            result.put("unscripted", Boolean.TRUE);
        }
        return result;
    }

    /**
     * Returns the connector ID.
     *
     * @return the connector ID
     */
    public String getConnectorId() {
        return connectorId;
    }

    /**
     * Returns the operation ID.
     *
     * @return the operation ID
     */
    public String getOperationId() {
        return operationId;
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
     * Returns the Dex Flow ID.
     *
     * @return the Flow ID
     */
    public String getFlowId() {
        return flowId;
    }

    /**
     * Returns the Call ID.
     *
     * @return the Call ID
     */
    public String getCallId() {
        return callId;
    }

    /**
     * Returns the operation input.
     *
     * @return the input
     */
    public I getInput() {
        return input;
    }

    /**
     * Returns the answered branch.
     *
     * @return the branch, or empty for a retry
     */
    public String getBranch() {
        return branch;
    }

    /**
     * Reports whether the call answered a retry.
     *
     * @return {@code true} for a retry
     */
    public boolean isRetry() {
        return isRetry;
    }

    /**
     * Reports whether the call replayed a terminal keyed result.
     *
     * @return {@code true} for a replay
     */
    public boolean isReplay() {
        return isReplay;
    }

    /**
     * Reports whether no script answered the call.
     *
     * @return {@code true} when unscripted
     */
    public boolean isUnscripted() {
        return isUnscripted;
    }
}
