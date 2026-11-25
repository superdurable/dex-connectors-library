// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import java.util.Objects;

/**
 * Stable operation route; only the happy path is required by default.
 *
 * <p>Every branch that is not the happy path is optional. An application that does not wire an
 * optional branch fails the Flow when the operation selects it.
 */
@JsonPropertyOrder({"id", "description", "optional"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class BranchDefinition {
    private final String id;
    private final String description;
    private final boolean optional;

    /**
     * Creates a branch definition; call {@link #validate()} before use.
     *
     * @param id the lower-camel branch ID
     * @param description the explanation shown to application authors
     * @param optional whether an application may leave this branch unwired
     */
    @JsonCreator
    public BranchDefinition(
            @JsonProperty(value = "id", required = true) final String id,
            @JsonProperty(value = "description", required = true) final String description,
            @JsonProperty("optional") final boolean optional) {
        this.id = id == null ? "" : id;
        this.description = description == null ? "" : description;
        this.optional = optional;
    }

    /**
     * Creates a required branch definition.
     *
     * @param id the lower-camel branch ID
     * @param description the explanation shown to application authors
     */
    public BranchDefinition(final String id, final String description) {
        this(id, description, false);
    }

    /**
     * Requires a lower-camel route and an explanatory description.
     *
     * @throws IllegalArgumentException if the ID or description is invalid
     */
    public void validate() {
        if (!OperationRef.OPERATION_ID_PATTERN.matcher(id).matches() || description.trim().isEmpty()) {
            throw new IllegalArgumentException("invalid branch identity or description");
        }
    }

    /**
     * Returns the lower-camel branch ID.
     *
     * @return the ID
     */
    @JsonProperty("id")
    public String getId() {
        return id;
    }

    /**
     * Returns the explanation shown to application authors.
     *
     * @return the description
     */
    @JsonProperty("description")
    public String getDescription() {
        return description;
    }

    /**
     * Reports whether an application may leave this branch unwired.
     *
     * @return {@code true} for an optional branch
     */
    @JsonProperty("optional")
    @JsonInclude(JsonInclude.Include.NON_DEFAULT)
    public boolean isOptional() {
        return optional;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof BranchDefinition)) {
            return false;
        }
        final BranchDefinition that = (BranchDefinition) other;
        return optional == that.optional && id.equals(that.id) && description.equals(that.description);
    }

    @Override
    public int hashCode() {
        return Objects.hash(id, description, optional);
    }
}
