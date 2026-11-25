// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

import com.fasterxml.jackson.databind.JsonNode;
import io.superdurable.dex.Context;
import io.superdurable.dexconnectors.sdk.StepAttemptValues;
import java.util.Collections;
import java.util.Map;
import java.util.TreeMap;

/** Merges the markers of every Fence in one Step attempt into each heartbeat. */
final class StepCheckpoint {
    private static final StepAttemptValues<StepCheckpoint> CHECKPOINTS =
            new StepAttemptValues<>(context -> new StepCheckpoint(previousAttemptMarkers(context)));

    private Map<String, Marker> markers;

    private StepCheckpoint(final Map<String, Marker> markers) {
        this.markers = markers;
    }

    static StepCheckpoint forContext(final Context context) {
        return CHECKPOINTS.valueFor(context);
    }

    synchronized void record(final Context context, final Marker marker, final boolean shouldRetainOnFailure) {
        final Map<String, Marker> updated = new TreeMap<>(markers);
        updated.put(marker.getCallId().value(), marker);
        try {
            context.recordHeartbeat(DispatchDecision.checkpointHeartbeat(updated));
        } catch (RuntimeException | Error error) {
            // A retained provider ID only forces read-back, so keeping it after a failure is safe.
            if (shouldRetainOnFailure) {
                markers = updated;
            }
            throw error;
        }
        markers = updated;
    }

    private static Map<String, Marker> previousAttemptMarkers(final Context context) {
        try {
            if (!context.hasLastHeartbeatValue()) {
                return Collections.emptyMap();
            }
            return DispatchDecision.markersFromHeartbeat(context.getLastHeartbeatValue(JsonNode.class));
        } catch (RuntimeException error) {
            // Seeding nothing loses entries, which only overcounts earlier sends.
            return Collections.emptyMap();
        }
    }
}
