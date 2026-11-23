// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import io.superdurable.dexconnectors.sdk.ConnectorConfigurationUI;
import java.util.Objects;

/**
 * One static application binding exposed to Flow Definition tooling and Dex Web.
 */
public final class TriggerBindingDefinition {
    private final TriggerDefinition definition;
    private final String connectionName;
    private final String bindingName;
    private final ConnectorConfigurationUI configurationUI;

    /**
     * Creates a binding definition; call {@link #validate()} or
     * {@link Triggers#mustTriggerBindingDefinition} before use.
     *
     * @param definition the immutable Trigger definition
     * @param connectionName the configured provider connection name
     * @param bindingName the configured Trigger binding name
     * @param configurationUI the optional configuration units, or {@code null}
     */
    public TriggerBindingDefinition(
            final TriggerDefinition definition, final String connectionName, final String bindingName,
            final ConnectorConfigurationUI configurationUI) {
        this.definition = Objects.requireNonNull(definition, "definition");
        this.connectionName = connectionName == null ? "" : connectionName;
        this.bindingName = bindingName == null ? "" : bindingName;
        this.configurationUI = configurationUI;
    }

    /**
     * Validates the definition, names, and configuration units.
     *
     * @throws IllegalArgumentException if any part is invalid
     */
    public void validate() {
        definition.validate();
        if (connectionName.trim().isEmpty()) {
            throw new IllegalArgumentException("trigger binding connection name is required");
        }
        if (bindingName.trim().isEmpty()) {
            throw new IllegalArgumentException("trigger binding name is required");
        }
        if (configurationUI != null) {
            configurationUI.validate();
        }
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
     * Returns the configured provider connection name.
     *
     * @return the connection name
     */
    public String getConnectionName() {
        return connectionName;
    }

    /**
     * Returns the configured Trigger binding name.
     *
     * @return the binding name
     */
    public String getBindingName() {
        return bindingName;
    }

    /**
     * Returns the optional configuration units.
     *
     * @return the configuration UI, or {@code null}
     */
    public ConnectorConfigurationUI getConfigurationUI() {
        return configurationUI;
    }
}
