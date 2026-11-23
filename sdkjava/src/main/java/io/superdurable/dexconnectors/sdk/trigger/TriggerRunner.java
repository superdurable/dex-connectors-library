// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * A configured long-running connector Trigger.
 */
public interface TriggerRunner {
    /**
     * Returns the immutable connector Trigger definition.
     *
     * @return the definition
     */
    TriggerDefinition getDefinition();

    /**
     * Returns the configured Trigger binding identity.
     *
     * @return the binding
     */
    TriggerBindingRef getBinding();

    /**
     * Processes provider events until the context is cancelled or an unrecoverable error.
     *
     * @param context the run context, or {@code null} for a context that is never cancelled
     * @throws Exception when the runner stops
     */
    void run(TriggerContext context) throws Exception;
}
