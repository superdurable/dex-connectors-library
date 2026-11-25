// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.CallId;
import java.util.Iterator;
import java.util.Map;
import java.util.TreeMap;

/**
 * Resend decision with the prior marker or a safe checkpoint error.
 *
 * <p>{@link #getMaximumPriorSends()} bounds the possibly effective sends made before this attempt;
 * it is zero for a fail-closed decision.
 */
public final class DispatchDecision {
    static final String CHECKPOINT_MARKERS_FIELD = "singleDispatchMarkers";

    private final DecisionKind kind;
    private final Marker marker;
    private final String error;
    private final int maximumPriorSends;

    DispatchDecision(final DecisionKind kind, final Marker marker, final String error, final int maximumPriorSends) {
        this.kind = kind;
        this.marker = marker;
        this.error = error == null ? "" : error;
        this.maximumPriorSends = maximumPriorSends;
    }

    /**
     * Permits a send only when proven safe or while the send cap allows another.
     *
     * <p>{@code previousHeartbeat} is the previous attempt's whole heartbeat value, or
     * {@code null} when it recorded none. Only this call's entry in the checkpoint map counts. A
     * single marker that an earlier release recorded as the whole heartbeat is this call's entry
     * when its Call ID matches. A missing entry, a legacy marker of another Call ID, or an
     * unreadable value counts as no marker.
     *
     * <p>No marker on attempt one permits the first send, as does a {@code notSent} or
     * {@code canResend} entry without provider ID from the immediately prior attempt. No marker on
     * a later attempt, or any other entry, demands read-back; when {@code maxSends} is at least two
     * and fewer than {@code maxSends} sends may have happened, it permits a resend after read-back
     * instead. An entry with an impossible send count fails closed.
     *
     * @param callId the Call ID
     * @param attempt the positive Dex Execute attempt
     * @param previousHeartbeat the previous attempt's heartbeat JSON, or {@code null}
     * @param maxSends the send cap within 1..10
     * @return the decision
     * @throws IllegalArgumentException if the Call ID, attempt, or cap is invalid
     */
    public static DispatchDecision decideDispatch(
            final CallId callId, final int attempt, final JsonNode previousHeartbeat, final int maxSends) {
        callId.validate();
        if (attempt < 1) {
            throw new IllegalArgumentException("single dispatch requires a positive attempt");
        }
        SendCaps.validateMaxSends(maxSends);
        final Marker marker = markersFromHeartbeat(previousHeartbeat).get(callId.value());
        if (marker == null) {
            if (attempt == 1) {
                return new DispatchDecision(DecisionKind.SEND_ALLOWED, null, "", 0);
            }
            return boundedReadBack(null, attempt - 1, maxSends);
        }
        if (!hasPossibleSendCount(marker)) {
            return new DispatchDecision(DecisionKind.FAIL_CLOSED, marker, "invalid single-dispatch checkpoint", 0);
        }
        if (marker.getAttempt() >= attempt) {
            return new DispatchDecision(DecisionKind.READ_BACK_REQUIRED, marker, "", marker.getSendCount());
        }
        final int maximumPriorSends = marker.getSendCount() + (attempt - 1 - marker.getAttempt());
        if (marker.getAttempt() == attempt - 1
                && (marker.getState() == DispatchState.NOT_SENT || marker.getState() == DispatchState.CAN_RESEND)
                && marker.getProviderId().isEmpty()) {
            return new DispatchDecision(DecisionKind.SEND_ALLOWED, marker, "", maximumPriorSends);
        }
        return boundedReadBack(marker, maximumPriorSends, maxSends);
    }

    /**
     * Returns the permitted action.
     *
     * @return the decision kind
     */
    public DecisionKind getKind() {
        return kind;
    }

    /**
     * Returns this call's prior checkpoint entry.
     *
     * @return the marker, or {@code null} when none was readable
     */
    public Marker getMarker() {
        return marker;
    }

    /**
     * Returns the safe checkpoint error of a fail-closed decision.
     *
     * @return the error, or empty
     */
    public String getError() {
        return error;
    }

    /**
     * Returns the upper bound of possibly effective sends before this attempt.
     *
     * @return the maximum prior sends
     */
    public int getMaximumPriorSends() {
        return maximumPriorSends;
    }

    static Map<String, Marker> markersFromHeartbeat(final JsonNode heartbeat) {
        final Map<String, Marker> markers = new TreeMap<>();
        if (heartbeat == null || !heartbeat.isObject()) {
            return markers;
        }
        if (!heartbeat.has(CHECKPOINT_MARKERS_FIELD)) {
            final Marker legacyMarker = markerFromEntry(heartbeat);
            if (legacyMarker != null) {
                markers.put(legacyMarker.getCallId().value(), legacyMarker);
            }
            return markers;
        }
        final JsonNode entries = heartbeat.get(CHECKPOINT_MARKERS_FIELD);
        if (!entries.isObject()) {
            return markers;
        }
        final Iterator<Map.Entry<String, JsonNode>> fields = entries.fields();
        while (fields.hasNext()) {
            final Map.Entry<String, JsonNode> field = fields.next();
            final Marker marker = markerFromEntry(field.getValue());
            if (marker != null && marker.getCallId().value().equals(field.getKey())) {
                markers.put(field.getKey(), marker);
            }
        }
        return markers;
    }

    static ObjectNode checkpointHeartbeat(final Map<String, Marker> markers) {
        final ObjectNode entries = JsonNodeFactory.instance.objectNode();
        for (Map.Entry<String, Marker> entry : new TreeMap<>(markers).entrySet()) {
            entries.set(entry.getKey(), entry.getValue().toCheckpointEntry());
        }
        final ObjectNode heartbeat = JsonNodeFactory.instance.objectNode();
        heartbeat.set(CHECKPOINT_MARKERS_FIELD, entries);
        return heartbeat;
    }

    private static Marker markerFromEntry(final JsonNode entry) {
        final Marker marker;
        try {
            marker = Marker.fromJson(entry);
        } catch (IllegalArgumentException error) {
            return null;
        }
        if (!entry.has("sendCount")) {
            // One send per attempt, excluding a notSent or canResend marker's own.
            return marker.withSendCount(
                    marker.getState() == DispatchState.CLAIMED ? marker.getAttempt() : marker.getAttempt() - 1);
        }
        return marker;
    }

    private static DispatchDecision boundedReadBack(final Marker marker, final int maximumPriorSends, final int maxSends) {
        final DecisionKind kind = maximumPriorSends < maxSends && maxSends > SendCaps.MINIMUM_MAX_SENDS
                ? DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
                : DecisionKind.READ_BACK_REQUIRED;
        return new DispatchDecision(kind, marker, "", maximumPriorSends);
    }

    private static boolean hasPossibleSendCount(final Marker marker) {
        if (marker.getState() == DispatchState.CLAIMED) {
            // A claim counts its own send.
            return marker.getSendCount() >= 1 && marker.getSendCount() <= marker.getAttempt();
        }
        return marker.getSendCount() <= marker.getAttempt() - 1;
    }
}
