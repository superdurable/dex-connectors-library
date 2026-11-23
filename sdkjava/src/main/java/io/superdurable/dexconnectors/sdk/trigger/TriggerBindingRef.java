// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import io.superdurable.dexconnectors.sdk.ConnectionRef;
import java.util.Objects;

/**
 * One configured use of a connector Trigger: its connection, Trigger, and stable binding name.
 */
public final class TriggerBindingRef {
    private final ConnectionRef connection;
    private final TriggerRef trigger;
    private final String name;

    /**
     * Creates a binding reference; call {@link #validate()} before use.
     *
     * @param connection the provider connection the Trigger reads
     * @param trigger the Trigger identity
     * @param name the stable application binding name
     */
    public TriggerBindingRef(final ConnectionRef connection, final TriggerRef trigger, final String name) {
        this.connection = Objects.requireNonNull(connection, "connection");
        this.trigger = Objects.requireNonNull(trigger, "trigger");
        this.name = name == null ? "" : name;
    }

    /**
     * Validates every identity part.
     *
     * @throws IllegalArgumentException if a part is invalid
     */
    public void validate() {
        connection.validate();
        trigger.validate();
        if (name.trim().isEmpty()) {
            throw new IllegalArgumentException("trigger binding name is required");
        }
    }

    /**
     * Returns the provider connection.
     *
     * @return the connection reference
     */
    public ConnectionRef getConnection() {
        return connection;
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
     * Returns the stable application binding name.
     *
     * @return the binding name
     */
    public String getName() {
        return name;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof TriggerBindingRef)) {
            return false;
        }
        final TriggerBindingRef that = (TriggerBindingRef) other;
        return connection.equals(that.connection) && trigger.equals(that.trigger) && name.equals(that.name);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(connection, trigger, name);
    }
}
