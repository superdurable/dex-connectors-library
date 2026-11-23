// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.util.Arrays;

/**
 * One acknowledged Trigger event that the application has not yet consumed.
 */
public final class PendingTriggerEvent {
    private final String id;
    private final byte[] event;

    /**
     * Creates a pending event.
     *
     * @param id the source's stable event identity
     * @param event the complete encoded event as raw JSON, kept exactly as stored
     */
    public PendingTriggerEvent(final String id, final byte[] event) {
        this.id = id == null ? "" : id;
        this.event = event == null ? new byte[0] : event.clone();
    }

    /**
     * Returns the event identity, decoded as Go decodes JSON strings.
     *
     * @return the event ID
     */
    public String getId() {
        return id;
    }

    /**
     * Returns a copy of the stored raw JSON event.
     *
     * @return the event bytes
     */
    public byte[] getEvent() {
        return event.clone();
    }

    byte[] eventBytes() {
        return event;
    }

    @Override
    public boolean equals(final Object other) {
        return other instanceof PendingTriggerEvent && id.equals(((PendingTriggerEvent) other).id)
                && Arrays.equals(event, ((PendingTriggerEvent) other).event);
    }

    @Override
    public int hashCode() {
        return 31 * id.hashCode() + Arrays.hashCode(event);
    }

    @Override
    public String toString() {
        return "PendingTriggerEvent{id=" + id + "}";
    }
}
