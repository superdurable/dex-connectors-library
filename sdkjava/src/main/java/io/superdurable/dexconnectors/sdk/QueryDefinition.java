// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Objects;
import java.util.Set;

/**
 * Query identity, stable routes, and Execute defaults.
 *
 * <p>A Query declares a {@code defect} branch and never an {@code uncertain} branch. A
 * {@code null} branch list differs from an empty one on the wire, as in Go.
 */
@JsonPropertyOrder({"operation", "branches", "stepDefaults"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class QueryDefinition {
    private final OperationRef operation;
    private final List<BranchDefinition> branches;
    private final StepDefaults stepDefaults;

    /**
     * Creates a Query definition; call {@link #validate()} before use.
     *
     * @param operation the operation identity
     * @param branches the declared routes, or {@code null} for none
     * @param stepDefaults the Execute defaults
     */
    @JsonCreator
    public QueryDefinition(
            @JsonProperty(value = "operation", required = true) final OperationRef operation,
            @JsonProperty(value = "branches", required = true) final List<BranchDefinition> branches,
            @JsonProperty(value = "stepDefaults", required = true) final StepDefaults stepDefaults) {
        this.operation = Objects.requireNonNull(operation, "operation");
        this.branches = branches == null ? null : Collections.unmodifiableList(new ArrayList<>(branches));
        this.stepDefaults = Objects.requireNonNull(stepDefaults, "stepDefaults");
    }

    /**
     * Requires distinct routes, a defect branch, and no uncertainty route.
     *
     * @throws IllegalArgumentException if the definition is invalid
     */
    public void validate() {
        operation.validate();
        if (branches != null && !branches.isEmpty()) {
            final Set<String> ids = branchIds(branches);
            if (!ids.contains(ConnectorBranches.DEFECT) || ids.contains(ConnectorBranches.UNCERTAIN)) {
                throw new IllegalArgumentException("invalid Query branches");
            }
        }
    }

    /**
     * Returns the operation identity.
     *
     * @return the operation
     */
    @JsonProperty("operation")
    public OperationRef getOperation() {
        return operation;
    }

    /**
     * Returns the declared routes.
     *
     * @return the unmodifiable branches, or {@code null} when none are declared
     */
    @JsonProperty("branches")
    @JsonInclude(JsonInclude.Include.ALWAYS)
    public List<BranchDefinition> getBranches() {
        return branches;
    }

    /**
     * Returns the Execute defaults.
     *
     * @return the Step defaults
     */
    @JsonProperty("stepDefaults")
    public StepDefaults getStepDefaults() {
        return stepDefaults;
    }

    static Set<String> branchIds(final List<BranchDefinition> branches) {
        final Set<String> ids = new HashSet<>();
        for (BranchDefinition branch : branches) {
            branch.validate();
            if (!ids.add(branch.getId())) {
                throw new IllegalArgumentException("duplicate branch " + branch.getId());
            }
        }
        return ids;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof QueryDefinition)) {
            return false;
        }
        final QueryDefinition that = (QueryDefinition) other;
        return operation.equals(that.operation)
                && Objects.equals(branches, that.branches)
                && stepDefaults.equals(that.stepDefaults);
    }

    @Override
    public int hashCode() {
        return Objects.hash(operation, branches, stepDefaults);
    }
}
