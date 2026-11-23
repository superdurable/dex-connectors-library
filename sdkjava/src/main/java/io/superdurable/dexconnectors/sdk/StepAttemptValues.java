// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import io.superdurable.dex.Context;
import java.util.Map;
import java.util.WeakHashMap;
import java.util.function.Function;

/**
 * Holds one in-process value per Dex Step Context and attempt.
 *
 * <p>Connector SDK packages use it to share state, such as call occurrence counts and
 * single-dispatch checkpoints, among every call of one Step attempt. A value disappears when its
 * Context is garbage collected, and a new attempt on the same Context object receives a fresh
 * value. Instances are thread-safe.
 *
 * @param <T> the per-attempt value type
 */
public final class StepAttemptValues<T> {
    private final Function<Context, T> createValue;
    private final Map<Context, AttemptValue<T>> valuesByContext = new WeakHashMap<>();

    /**
     * Creates a registry that builds missing values with the given function.
     *
     * @param createValue builds the value for a new Context or attempt
     */
    public StepAttemptValues(final Function<Context, T> createValue) {
        if (createValue == null) {
            throw new IllegalArgumentException("createValue is required");
        }
        this.createValue = createValue;
    }

    /**
     * Returns the Context's value for its current attempt, creating it when absent.
     *
     * @param context the Dex Step Context
     * @return the per-attempt value
     */
    public synchronized T valueFor(final Context context) {
        if (context == null) {
            throw new IllegalArgumentException("Dex Step Context is required");
        }
        final int attempt = context.getAttempt();
        final AttemptValue<T> existing = valuesByContext.get(context);
        if (existing != null && existing.attempt == attempt) {
            return existing.value;
        }
        final T value = createValue.apply(context);
        valuesByContext.put(context, new AttemptValue<T>(attempt, value));
        return value;
    }

    private static final class AttemptValue<T> {
        private final int attempt;
        private final T value;

        private AttemptValue(final int attempt, final T value) {
            this.attempt = attempt;
            this.value = value;
        }
    }
}
