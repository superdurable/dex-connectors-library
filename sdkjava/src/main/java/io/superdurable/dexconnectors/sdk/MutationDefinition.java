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
import java.util.List;
import java.util.Objects;
import java.util.Set;

/**
 * Mutation identity, stable routes, Execute defaults, and the unknown-outcome resend tier.
 *
 * <p>{@link #getRepeatOnUnknown()} and {@link #getMaxSends()} carry the manifest
 * {@code execution.repeatOnUnknown} and {@code execution.maxSends}. A {@code null} tier means the
 * definition predates these fields; a Step then rejects a send-cap override and passes a cap of
 * 1. The max sends value is 2..10 for {@link RepeatOnUnknown#LIMITED} and 0 otherwise. Neither
 * field is part of the definition's wire encoding.
 */
@JsonPropertyOrder({"operation", "branches", "stepDefaults"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class MutationDefinition {
    private final OperationRef operation;
    private final List<BranchDefinition> branches;
    private final StepDefaults stepDefaults;
    private final RepeatOnUnknown repeatOnUnknown;
    private final int maxSends;

    /**
     * Creates a Mutation definition with a resend tier; call {@link #validate()} before use.
     *
     * @param operation the operation identity
     * @param branches the declared routes, or {@code null} for none
     * @param stepDefaults the Execute defaults
     * @param repeatOnUnknown the manifest resend tier, or {@code null} when undeclared
     * @param maxSends the send cap for a limited tier, otherwise 0
     */
    public MutationDefinition(
            final OperationRef operation,
            final List<BranchDefinition> branches,
            final StepDefaults stepDefaults,
            final RepeatOnUnknown repeatOnUnknown,
            final int maxSends) {
        this.operation = Objects.requireNonNull(operation, "operation");
        this.branches = branches == null ? null : Collections.unmodifiableList(new ArrayList<>(branches));
        this.stepDefaults = Objects.requireNonNull(stepDefaults, "stepDefaults");
        this.repeatOnUnknown = repeatOnUnknown;
        this.maxSends = maxSends;
    }

    /**
     * Creates a Mutation definition without a declared resend tier.
     *
     * @param operation the operation identity
     * @param branches the declared routes, or {@code null} for none
     * @param stepDefaults the Execute defaults
     */
    @JsonCreator
    public MutationDefinition(
            @JsonProperty(value = "operation", required = true) final OperationRef operation,
            @JsonProperty(value = "branches", required = true) final List<BranchDefinition> branches,
            @JsonProperty(value = "stepDefaults", required = true) final StepDefaults stepDefaults) {
        this(operation, branches, stepDefaults, null, 0);
    }

    /**
     * Requires distinct routes, a defect branch, and a consistent resend tier.
     *
     * @throws IllegalArgumentException if the definition is invalid
     */
    public void validate() {
        operation.validate();
        if (repeatOnUnknown == RepeatOnUnknown.LIMITED) {
            if (maxSends < 2 || maxSends > 10) {
                throw new IllegalArgumentException("limited Mutation maxSends must be an integer within 2..10");
            }
        } else if (maxSends != 0) {
            throw new IllegalArgumentException("Mutation maxSends requires repeatOnUnknown limited");
        }
        if (branches != null && !branches.isEmpty()) {
            final Set<String> ids = QueryDefinition.branchIds(branches);
            if (!ids.contains(ConnectorBranches.DEFECT)) {
                throw new IllegalArgumentException("invalid Mutation branches");
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

    /**
     * Returns the manifest resend tier.
     *
     * @return the tier, or {@code null} when the definition predates it
     */
    public RepeatOnUnknown getRepeatOnUnknown() {
        return repeatOnUnknown;
    }

    /**
     * Returns the manifest send cap of a limited operation.
     *
     * @return 2..10 for a limited tier, otherwise 0
     */
    public int getMaxSends() {
        return maxSends;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof MutationDefinition)) {
            return false;
        }
        final MutationDefinition that = (MutationDefinition) other;
        return maxSends == that.maxSends
                && repeatOnUnknown == that.repeatOnUnknown
                && operation.equals(that.operation)
                && Objects.equals(branches, that.branches)
                && stepDefaults.equals(that.stepDefaults);
    }

    @Override
    public int hashCode() {
        return Objects.hash(operation, branches, stepDefaults, repeatOnUnknown, maxSends);
    }
}
