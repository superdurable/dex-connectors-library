// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * One reusable catalog configuration unit in application composition order.
 */
@JsonPropertyOrder({"id", "unitId", "label", "description", "required", "bindings"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class ConnectorUIUnit {
    private final String id;
    private final String unitId;
    private final String label;
    private final String description;
    private final boolean required;
    private final List<ConnectorUIBinding> bindings;

    /**
     * Creates a unit; call {@link #validate()} before use.
     *
     * @param id the lower-camel use ID within the Step or Trigger
     * @param unitId the lower-camel released catalog unit ID
     * @param label the field label
     * @param description guidance in the context of this operation or Trigger
     * @param required whether the user must configure the unit
     * @param bindings the port bindings
     */
    @JsonCreator
    public ConnectorUIUnit(
            @JsonProperty(value = "id", required = true) final String id,
            @JsonProperty(value = "unitId", required = true) final String unitId,
            @JsonProperty(value = "label", required = true) final String label,
            @JsonProperty(value = "description", required = true) final String description,
            @JsonProperty(value = "required", required = true) final boolean required,
            @JsonProperty(value = "bindings", required = true) final List<ConnectorUIBinding> bindings) {
        this.id = id == null ? "" : id;
        this.unitId = unitId == null ? "" : unitId;
        this.label = label == null ? "" : label;
        this.description = description == null ? "" : description;
        this.required = required;
        this.bindings = bindings == null ? Collections.<ConnectorUIBinding>emptyList()
                : Collections.unmodifiableList(new ArrayList<>(bindings));
    }

    /**
     * Checks static unit guidance and unique ports.
     *
     * @throws IllegalArgumentException if the unit is invalid
     */
    public void validate() {
        if (!ConnectorConfigurationUI.IDENTIFIER.matcher(id).matches()
                || !ConnectorConfigurationUI.IDENTIFIER.matcher(unitId).matches()) {
            throw new IllegalArgumentException("Connector UI unit IDs must be lower camel case");
        }
        if (label.trim().isEmpty() || description.trim().isEmpty() || bindings.isEmpty()) {
            throw new IllegalArgumentException("Connector UI unit needs a label, description, and bindings");
        }
        final Set<String> ports = new HashSet<>();
        for (ConnectorUIBinding binding : bindings) {
            binding.validate();
            if (!ports.add(binding.getPort())) {
                throw new IllegalArgumentException("Connector UI unit ports must be unique");
            }
        }
    }

    /**
     * Returns the use ID.
     *
     * @return the ID
     */
    @JsonProperty("id")
    public String getId() {
        return id;
    }

    /**
     * Returns the released catalog unit ID.
     *
     * @return the unit ID
     */
    @JsonProperty("unitId")
    public String getUnitId() {
        return unitId;
    }

    /**
     * Returns the field label.
     *
     * @return the label
     */
    @JsonProperty("label")
    public String getLabel() {
        return label;
    }

    /**
     * Returns the guidance shown with the unit.
     *
     * @return the description
     */
    @JsonProperty("description")
    public String getDescription() {
        return description;
    }

    /**
     * Reports whether the user must configure the unit.
     *
     * @return {@code true} when required
     */
    @JsonProperty("required")
    public boolean isRequired() {
        return required;
    }

    /**
     * Returns the port bindings.
     *
     * @return the unmodifiable bindings
     */
    @JsonProperty("bindings")
    public List<ConnectorUIBinding> getBindings() {
        return bindings;
    }
}
