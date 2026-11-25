// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * One provider source and application target, validated by {@link Triggers#newTrigger}.
 *
 * @param <P> the payload type
 */
public final class TriggerConfig<P> {
    private final TriggerDefinition definition;
    private final TriggerBindingRef binding;
    private final TriggerSource<P> source;
    private final TriggerTarget<P> target;
    private final TriggerLogger logger;

    /**
     * Creates a configuration.
     *
     * @param definition the immutable Trigger definition
     * @param binding the configured binding; its Trigger must equal the definition's
     * @param source the provider source
     * @param target the application target
     * @param logger the runner's record destination, or {@code null} for the default
     */
    public TriggerConfig(
            final TriggerDefinition definition, final TriggerBindingRef binding, final TriggerSource<P> source,
            final TriggerTarget<P> target, final TriggerLogger logger) {
        this.definition = definition;
        this.binding = binding;
        this.source = source;
        this.target = target;
        this.logger = logger;
    }

    /**
     * Returns the Trigger definition.
     *
     * @return the definition
     */
    public TriggerDefinition getDefinition() {
        return definition;
    }

    /**
     * Returns the binding identity.
     *
     * @return the binding
     */
    public TriggerBindingRef getBinding() {
        return binding;
    }

    /**
     * Returns the provider source.
     *
     * @return the source
     */
    public TriggerSource<P> getSource() {
        return source;
    }

    /**
     * Returns the application target.
     *
     * @return the target
     */
    public TriggerTarget<P> getTarget() {
        return target;
    }

    /**
     * Returns the record destination.
     *
     * @return the logger, or {@code null} for the default
     */
    public TriggerLogger getLogger() {
        return logger;
    }
}
