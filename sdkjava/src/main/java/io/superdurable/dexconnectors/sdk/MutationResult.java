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
 * Validated terminal Mutation route with the same wire shape as {@link QueryResult}.
 *
 * <p>Each operation publishes a concrete subclass, such as
 * {@code SendMessageResult extends MutationResult<SendMessageOutput>}, for use as a Dex Step input
 * type. The {@code uncertain} branch always carries a failure.
 *
 * @param <T> the operation output type
 */
@JsonPropertyOrder({"branch", "value", "receipt", "failure"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public class MutationResult<T> {
    private final String branch;
    private final T value;
    private final Receipt receipt;
    private final Failure failure;

    /**
     * Creates a validated Mutation result.
     *
     * @param branch the declared terminal branch
     * @param value the operation output, which may be {@code null}
     * @param receipt the completed receipt
     * @param failure the secret-safe failure, required for {@code uncertain}
     * @throws IllegalArgumentException if the branch, receipt, or uncertainty failure is invalid
     */
    @JsonCreator
    public MutationResult(
            @JsonProperty(value = "branch", required = true) final String branch,
            @JsonProperty("value") final T value,
            @JsonProperty(value = "receipt", required = true) final Receipt receipt,
            @JsonProperty("failure") final Failure failure) {
        if (branch == null || branch.isEmpty() || receipt == null
                || (ConnectorBranches.UNCERTAIN.equals(branch) && failure == null)) {
            throw new IllegalArgumentException("Mutation result branch is invalid");
        }
        this.branch = branch;
        this.value = value;
        this.receipt = receipt;
        this.failure = failure;
    }

    /**
     * Returns the terminal branch.
     *
     * @return the branch ID
     */
    @JsonProperty("branch")
    public final String getBranch() {
        return branch;
    }

    /**
     * Returns the operation output.
     *
     * @return the value, possibly {@code null}
     */
    @JsonProperty("value")
    @JsonInclude(JsonInclude.Include.ALWAYS)
    public final T getValue() {
        return value;
    }

    /**
     * Returns the completed receipt.
     *
     * @return the receipt
     */
    @JsonProperty("receipt")
    public final Receipt getReceipt() {
        return receipt;
    }

    /**
     * Returns the secret-safe failure of a non-happy branch.
     *
     * @return the failure, or {@code null}
     */
    @JsonProperty("failure")
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public final Failure getFailure() {
        return failure;
    }

    @Override
    public boolean equals(final Object other) {
        if (other == null || other.getClass() != getClass()) {
            return false;
        }
        final MutationResult<?> that = (MutationResult<?>) other;
        return branch.equals(that.branch)
                && Objects.equals(value, that.value)
                && receipt.equals(that.receipt)
                && Objects.equals(failure, that.failure);
    }

    @Override
    public int hashCode() {
        return Objects.hash(branch, value, receipt, failure);
    }

    @Override
    public String toString() {
        return getClass().getSimpleName() + "{branch=" + branch + ", failure=" + failure + "}";
    }
}
