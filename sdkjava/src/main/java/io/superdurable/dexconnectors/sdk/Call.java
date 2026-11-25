// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import io.superdurable.dex.BufferedTextStream;
import io.superdurable.dex.Context;
import io.superdurable.dex.Stream;
import java.util.Objects;

/**
 * Provider-safe metadata and the ephemeral Dex Context of one operation invocation.
 *
 * <p>The live Context is available only during a Step handler and is never serialized, compared,
 * or included in Flow state. Only the Call ID, connection, operation, and idempotency key cross
 * the JSON boundary. Progress and text writes are no-ops when the Step was not configured with
 * the corresponding Stream.
 */
@JsonPropertyOrder({"id", "idempotencyKey", "connection", "operation"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class Call {
    private final CallId id;
    private final ConnectionRef connection;
    private final OperationRef operation;
    private final String idempotencyKey;
    private final transient Context context;
    private final transient ProgressReporter progressReporter;
    private final transient BufferedTextStream textWriter;
    private final int maxSends;

    private Call(
            final CallId id, final ConnectionRef connection, final OperationRef operation,
            final String idempotencyKey, final Context context, final ProgressReporter progressReporter,
            final BufferedTextStream textWriter, final int maxSends) {
        this.id = Objects.requireNonNull(id, "id");
        this.connection = Objects.requireNonNull(connection, "connection");
        this.operation = Objects.requireNonNull(operation, "operation");
        this.idempotencyKey = idempotencyKey == null ? "" : idempotencyKey;
        this.context = context;
        this.progressReporter = progressReporter;
        this.textWriter = textWriter;
        this.maxSends = maxSends;
    }

    @JsonCreator
    static Call fromWire(
            @JsonProperty(value = "id", required = true) final CallId id,
            @JsonProperty(value = "connection", required = true) final ConnectionRef connection,
            @JsonProperty(value = "operation", required = true) final OperationRef operation,
            @JsonProperty("idempotencyKey") final String idempotencyKey) {
        id.validate();
        connection.validate();
        operation.validate();
        return new Call(id, connection, operation, idempotencyKey, null, null, null, 1);
    }

    /**
     * Creates a Call outside a Dex Step, such as for a Trigger's credential resolution or a test.
     *
     * <p>The Call has no Dex Context, so progress and text writes are no-ops. For a Trigger, the
     * operation ID is the Trigger name.
     *
     * @param connection the logical connection
     * @param operation the operation or Trigger identity
     * @param callId the Call ID
     * @return the Call
     * @throws IllegalArgumentException if a reference or the Call ID is invalid
     */
    public static Call newTriggerCall(final ConnectionRef connection, final OperationRef operation, final CallId callId) {
        connection.validate();
        operation.validate();
        callId.validate();
        return new Call(callId, connection, operation, "", null, null, null, 1);
    }

    /**
     * Creates a Call with an explicit identity, such as in a connector test with a fake Context.
     *
     * <p>Production Steps use {@link #forStep} so the Call ID is derived from the Dex Step.
     *
     * @param callId the Call ID
     * @param connection the logical connection
     * @param operation the operation identity
     * @param context the Dex Step Context, or {@code null}
     * @param maxSends the single-dispatch send cap within 1..10
     * @return the Call
     * @throws IllegalArgumentException if a reference or the Call ID is invalid
     */
    public static Call create(
            final CallId callId, final ConnectionRef connection, final OperationRef operation,
            final Context context, final int maxSends) {
        connection.validate();
        operation.validate();
        callId.validate();
        return new Call(callId, connection, operation, "", context, null, null, maxSends);
    }

    /**
     * Creates a Call for the next call of an operation in the current Dex Step attempt.
     *
     * <p>The Call ID comes from {@link ConnectorSteps#deriveStepCallId}. Step factories call this;
     * a hand-written Step that runs an operation directly calls it once per call, in a
     * deterministic order.
     *
     * @param context the Dex Step Context
     * @param connection the logical connection
     * @param operation the operation identity
     * @param maxSends the single-dispatch send cap within 1..10
     * @return the Call bound to the Context
     */
    public static Call forStep(
            final Context context, final ConnectionRef connection, final OperationRef operation, final int maxSends) {
        return forStep(context, connection, operation, maxSends, null, null);
    }

    static Call forStep(
            final Context context, final ConnectionRef connection, final OperationRef operation,
            final int maxSends, final ProgressReporterFactory progressReporterFactory,
            final BufferedTextStream textWriter) {
        connection.validate();
        final CallId callId = ConnectorSteps.deriveStepCallId(context, operation);
        return new Call(callId, connection, operation, "", context,
                progressReporterFactory == null ? null : progressReporterFactory.create(context, callId),
                textWriter, maxSends);
    }

    /**
     * Returns a copy carrying the resolved idempotency key.
     *
     * @param key the provider idempotency key
     * @return the Call with the key
     */
    Call withIdempotencyKey(final String key) {
        return new Call(id, connection, operation, key, context, progressReporter, textWriter, maxSends);
    }

    /**
     * Returns the stable Call ID.
     *
     * @return the Call ID
     */
    @JsonProperty("id")
    public CallId getId() {
        return id;
    }

    /**
     * Returns the logical connection.
     *
     * @return the connection reference
     */
    @JsonProperty("connection")
    public ConnectionRef getConnection() {
        return connection;
    }

    /**
     * Returns the operation identity.
     *
     * @return the operation reference
     */
    @JsonProperty("operation")
    public OperationRef getOperation() {
        return operation;
    }

    /**
     * Returns the provider idempotency key of a Mutation call.
     *
     * @return the key, or empty for a Query
     */
    @JsonProperty("idempotencyKey")
    @JsonInclude(JsonInclude.Include.NON_EMPTY)
    public String getIdempotencyKey() {
        return idempotencyKey;
    }

    /**
     * Returns the live Dex Context of the current Step attempt.
     *
     * <p>Use it only on the handler thread and never retain it after the operation returns.
     *
     * @return the Context, or {@code null} outside a Dex Step
     */
    public Context getContext() {
        return context;
    }

    /**
     * Returns the single-dispatch send cap for this Step execution.
     *
     * <p>A Mutation Step sets the manifest {@code maxSends} of a {@code limited} operation, or the
     * application's override; otherwise it is 1. Pass it to the single-dispatch Fence. It is not
     * part of the persisted Call identity.
     *
     * @return the send cap within 1..10
     */
    public int getMaxSends() {
        return maxSends;
    }

    /**
     * Appends structured progress when the consuming Flow registered a progress Stream.
     *
     * @param progress the provider-safe progress
     * @throws IllegalArgumentException if the percent is outside 0..1
     */
    public void reportProgress(final Progress progress) {
        if (progressReporter != null) {
            progressReporter.report(progress);
        }
    }

    /**
     * Appends ordered text when a buffered text Stream is configured.
     *
     * @param value the text chunk
     */
    public void writeText(final String value) {
        if (textWriter != null) {
            textWriter.write(value);
        }
    }

    /**
     * Reports whether this invocation can emit structured progress.
     *
     * @return {@code true} when a progress Stream is configured
     */
    public boolean hasProgressStream() {
        return progressReporter != null;
    }

    /**
     * Reports whether this invocation can emit buffered text.
     *
     * @return {@code true} when a text Stream is configured
     */
    public boolean hasTextStream() {
        return textWriter != null;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof Call)) {
            return false;
        }
        final Call that = (Call) other;
        return id.equals(that.id) && connection.equals(that.connection) && operation.equals(that.operation)
                && idempotencyKey.equals(that.idempotencyKey);
    }

    @Override
    public int hashCode() {
        return Objects.hash(id, connection, operation, idempotencyKey);
    }

    interface ProgressReporterFactory {
        ProgressReporter create(Context context, CallId callId);
    }

    static final class ProgressReporter {
        private final Stream<ProgressUpdate> stream;
        private final Context context;
        private final CallId callId;
        private long sequence;

        ProgressReporter(final Stream<ProgressUpdate> stream, final Context context, final CallId callId) {
            this.stream = stream;
            this.context = context;
            this.callId = callId;
        }

        synchronized void report(final Progress progress) {
            final Double percent = progress.getPercent();
            if (percent != null && (percent < 0 || percent > 1)) {
                throw new IllegalArgumentException("connector progress percent must be between 0 and 1");
            }
            sequence++;
            stream.write(context, new ProgressUpdate(callId, context.getAttempt(), sequence, progress.getPhase(),
                    progress.getMessage(), percent, progress.getProviderSequence()));
        }
    }
}
