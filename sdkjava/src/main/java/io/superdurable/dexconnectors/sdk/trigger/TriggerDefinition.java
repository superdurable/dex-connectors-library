// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import java.util.Objects;

/**
 * One provider event source declared by a connector manifest.
 */
public final class TriggerDefinition {
    private final TriggerRef trigger;
    private final String description;

    /**
     * Creates a Trigger definition; call {@link #validate()} before use.
     *
     * @param trigger the Trigger identity
     * @param description the application-facing description
     */
    public TriggerDefinition(final TriggerRef trigger, final String description) {
        this.trigger = Objects.requireNonNull(trigger, "trigger");
        this.description = description == null ? "" : description;
    }

    /**
     * Validates the identity and requires a description.
     *
     * @throws IllegalArgumentException if the definition is invalid
     */
    public void validate() {
        trigger.validate();
        if (description.trim().isEmpty()) {
            throw new IllegalArgumentException("trigger description is required");
        }
    }

    /**
     * Returns the Trigger identity.
     *
     * @return the Trigger reference
     */
    public TriggerRef getTrigger() {
        return trigger;
    }

    /**
     * Returns the application-facing description.
     *
     * @return the description
     */
    public String getDescription() {
        return description;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        return other instanceof TriggerDefinition && trigger.equals(((TriggerDefinition) other).trigger)
                && description.equals(((TriggerDefinition) other).description);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(trigger, description);
    }
}
