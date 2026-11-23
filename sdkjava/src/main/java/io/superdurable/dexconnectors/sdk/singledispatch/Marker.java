// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.CallId;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Iterator;
import java.util.Objects;
import java.util.Set;

/**
 * Non-secret Dex heartbeat checkpoint of one logical call and attempt.
 *
 * <p>The send count is the number of possibly effective sends this Call ID made up to and
 * including the attempt. Every Fence writes the exact count into its checkpoint entry, zero
 * included. {@link #toJson()} omits a zero count and an empty provider ID; a checkpoint entry
 * without the count is read as the attempt for a claim and the attempt minus one otherwise, which
 * can only overcount.
 */
public final class Marker {
    private static final Set<String> FIELDS =
            new HashSet<>(Arrays.asList("callId", "attempt", "state", "providerId", "sendCount"));

    private final CallId callId;
    private final int attempt;
    private final DispatchState state;
    private final String providerId;
    private final int sendCount;

    /**
     * Creates a validated marker.
     *
     * @param callId the Call ID
     * @param attempt the positive Dex Execute attempt
     * @param state the dispatch state
     * @param providerId the accepted provider resource ID, or empty
     * @param sendCount the nonnegative possibly effective send count
     * @throws IllegalArgumentException if a value is invalid
     */
    public Marker(
            final CallId callId, final int attempt, final DispatchState state, final String providerId,
            final int sendCount) {
        if (callId == null || state == null || attempt < 1 || sendCount < 0) {
            throw new IllegalArgumentException("invalid single-dispatch checkpoint");
        }
        callId.validate();
        this.callId = callId;
        this.attempt = attempt;
        this.state = state;
        this.providerId = providerId == null ? "" : providerId;
        this.sendCount = sendCount;
    }

    /**
     * Decodes only declared marker fields and fails closed on malformed data.
     *
     * @param value the marker JSON
     * @return the marker
     * @throws IllegalArgumentException if the value is not a valid marker
     */
    public static Marker fromJson(final JsonNode value) {
        if (value == null || !value.isObject()
                || !value.has("callId") || !value.has("attempt") || !value.has("state")) {
            throw new IllegalArgumentException("invalid single-dispatch checkpoint");
        }
        final Iterator<String> names = value.fieldNames();
        while (names.hasNext()) {
            if (!FIELDS.contains(names.next())) {
                throw new IllegalArgumentException("invalid single-dispatch checkpoint");
            }
        }
        final JsonNode providerId = value.get("providerId");
        final JsonNode sendCount = value.get("sendCount");
        if (!value.get("callId").isTextual() || !isPlainInt(value.get("attempt"))
                || !value.get("state").isTextual()
                || (providerId != null && !providerId.isTextual())
                || (sendCount != null && !isPlainInt(sendCount))) {
            throw new IllegalArgumentException("invalid single-dispatch checkpoint");
        }
        final DispatchState state = DispatchState.fromWireValue(value.get("state").textValue());
        if (state == null) {
            throw new IllegalArgumentException("invalid single-dispatch checkpoint");
        }
        try {
            return new Marker(CallId.of(value.get("callId").textValue()), value.get("attempt").intValue(), state,
                    providerId == null ? "" : providerId.textValue(), sendCount == null ? 0 : sendCount.intValue());
        } catch (IllegalArgumentException error) {
            throw new IllegalArgumentException("invalid single-dispatch checkpoint", error);
        }
    }

    /**
     * Encodes Go's camel-case marker, omitting an empty provider ID and a zero send count.
     *
     * @return the marker JSON
     */
    public ObjectNode toJson() {
        final ObjectNode result = JsonNodeFactory.instance.objectNode();
        result.put("callId", callId.value());
        result.put("attempt", attempt);
        result.put("state", state.getWireValue());
        if (!providerId.isEmpty()) {
            result.put("providerId", providerId);
        }
        if (sendCount != 0) {
            result.put("sendCount", sendCount);
        }
        return result;
    }

    ObjectNode toCheckpointEntry() {
        final ObjectNode entry = JsonNodeFactory.instance.objectNode();
        entry.put("callId", callId.value());
        entry.put("attempt", attempt);
        entry.put("state", state.getWireValue());
        if (!providerId.isEmpty()) {
            entry.put("providerId", providerId);
        }
        entry.put("sendCount", sendCount);
        return entry;
    }

    Marker withSendCount(final int value) {
        return new Marker(callId, attempt, state, providerId, value);
    }

    /**
     * Returns the Call ID.
     *
     * @return the Call ID
     */
    public CallId getCallId() {
        return callId;
    }

    /**
     * Returns the Dex Execute attempt.
     *
     * @return the attempt
     */
    public int getAttempt() {
        return attempt;
    }

    /**
     * Returns the dispatch state.
     *
     * @return the state
     */
    public DispatchState getState() {
        return state;
    }

    /**
     * Returns the accepted provider resource ID.
     *
     * @return the provider ID, or empty
     */
    public String getProviderId() {
        return providerId;
    }

    /**
     * Returns the possibly effective send count through this attempt.
     *
     * @return the send count
     */
    public int getSendCount() {
        return sendCount;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof Marker)) {
            return false;
        }
        final Marker that = (Marker) other;
        return attempt == that.attempt && sendCount == that.sendCount && state == that.state
                && callId.equals(that.callId) && providerId.equals(that.providerId);
    }

    @Override
    public int hashCode() {
        return Objects.hash(callId, attempt, state, providerId, sendCount);
    }

    @Override
    public String toString() {
        return toJson().toString();
    }

    private static boolean isPlainInt(final JsonNode value) {
        return value != null && value.isIntegralNumber() && value.canConvertToInt();
    }
}
