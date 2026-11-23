// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import java.util.concurrent.atomic.AtomicBoolean;

/**
 * One delivery attempt started with {@link TriggerDelivery#startTriggerAttempt}.
 */
public final class TriggerAttempt {
    private final TriggerContext context;
    private final AtomicBoolean isSkipped;

    TriggerAttempt(final TriggerContext context, final AtomicBoolean isSkipped) {
        this.context = context;
        this.isSkipped = isSkipped;
    }

    /**
     * Returns the context to pass to the target for this attempt.
     *
     * @return the attempt context
     */
    public TriggerContext getContext() {
        return context;
    }

    /**
     * Reports whether a component inside the attempt consumed the event as undeliverable.
     *
     * @return {@code true} after a recorded skip
     */
    public boolean wasSkipped() {
        return isSkipped.get();
    }
}
