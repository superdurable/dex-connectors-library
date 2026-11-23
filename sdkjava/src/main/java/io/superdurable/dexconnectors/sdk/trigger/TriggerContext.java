// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Per-delivery cancellation, log attributes, and skip recording, passed to every target call.
 *
 * <p>Create the root with {@code new TriggerContext()} and derive attempts with
 * {@link TriggerDelivery#startTriggerAttempt}. Instances are immutable apart from the shared
 * cancellation.
 */
public final class TriggerContext {
    private final TriggerCancellation cancellation;
    private final Map<String, Object> logAttributes;
    private final Runnable onSkip;

    /**
     * Creates a root context that is never cancelled.
     */
    public TriggerContext() {
        this(new TriggerCancellation());
    }

    /**
     * Creates a root context with a cancellation.
     *
     * @param cancellation the cancellation that ends delivery
     */
    public TriggerContext(final TriggerCancellation cancellation) {
        this(cancellation, Collections.<String, Object>emptyMap(), null);
    }

    TriggerContext(final TriggerCancellation cancellation, final Map<String, Object> logAttributes, final Runnable onSkip) {
        if (cancellation == null) {
            throw new IllegalArgumentException("Trigger cancellation is required");
        }
        this.cancellation = cancellation;
        this.logAttributes = Collections.unmodifiableMap(new LinkedHashMap<>(logAttributes));
        this.onSkip = onSkip;
    }

    /**
     * Derives a context that shares log attributes and skip recording with a new cancellation.
     *
     * @param cancellation the derived cancellation, normally a child of this context's
     * @return the derived context
     */
    public TriggerContext withCancellation(final TriggerCancellation cancellation) {
        return new TriggerContext(cancellation, logAttributes, this::recordSkip);
    }

    /**
     * Returns the cancellation that ends delivery: sources stop, retries stop waiting, and late
     * inbox writes are refused.
     *
     * @return the cancellation
     */
    public TriggerCancellation getCancellation() {
        return cancellation;
    }

    /**
     * Returns event-scoped IDs added to every delivery record.
     *
     * @return the unmodifiable log attributes
     */
    public Map<String, Object> getLogAttributes() {
        return logAttributes;
    }

    /**
     * Returns a context whose records also carry the attributes; never pass message text or payloads.
     *
     * @param attributes the safe attributes to add
     * @return the derived context
     */
    public TriggerContext withLogAttributes(final Map<String, Object> attributes) {
        final Map<String, Object> merged = new LinkedHashMap<>(logAttributes);
        merged.putAll(attributes);
        return new TriggerContext(cancellation, merged, this::recordSkip);
    }

    /**
     * Records that a component in this attempt consumed the event as undeliverable and logged it.
     */
    public void recordSkip() {
        if (onSkip != null) {
            onSkip.run();
        }
    }
}
