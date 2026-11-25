// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import java.time.OffsetDateTime;
import java.util.Objects;

/**
 * One provider event with a stable provider identity.
 *
 * @param <P> the connector-decoded payload type
 */
public final class TriggerEvent<P> {
    private final String id;
    private final OffsetDateTime occurredAt;
    private final P payload;

    /**
     * Creates an event.
     *
     * @param id the stable provider identifier; also the Flow-start request ID
     * @param occurredAt when the provider event occurred
     * @param payload the connector-decoded provider event body
     */
    public TriggerEvent(final String id, final OffsetDateTime occurredAt, final P payload) {
        this.id = id == null ? "" : id;
        this.occurredAt = Objects.requireNonNull(occurredAt, "occurredAt");
        this.payload = payload;
    }

    /**
     * Returns the stable provider identifier.
     *
     * @return the event ID
     */
    public String getId() {
        return id;
    }

    /**
     * Returns when the provider event occurred.
     *
     * @return the occurrence time with its offset
     */
    public OffsetDateTime getOccurredAt() {
        return occurredAt;
    }

    /**
     * Returns the connector-decoded provider event body.
     *
     * @return the payload
     */
    public P getPayload() {
        return payload;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof TriggerEvent)) {
            return false;
        }
        final TriggerEvent<?> that = (TriggerEvent<?>) other;
        return id.equals(that.id) && occurredAt.equals(that.occurredAt) && Objects.equals(payload, that.payload);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(id, occurredAt, payload);
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return "TriggerEvent{id=" + id + ", occurredAt=" + occurredAt + "}";
    }
}
