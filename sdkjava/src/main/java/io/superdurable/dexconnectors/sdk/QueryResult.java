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
 * Validated terminal Query route, typed value, receipt, and optional failure.
 *
 * <p>A Dex Step input type must be a concrete class, so each operation publishes a concrete
 * subclass such as {@code GetMessageResult extends QueryResult<GetMessageOutput>}; a Step that
 * receives the result declares that subclass as its input type. The {@code value} member is
 * always encoded, including {@code null}, and {@code failure} is omitted when absent.
 *
 * @param <T> the operation output type
 */
@JsonPropertyOrder({"branch", "value", "receipt", "failure"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public class QueryResult<T> {
    private final String branch;
    private final T value;
    private final Receipt receipt;
    private final Failure failure;

    /**
     * Creates a validated Query result.
     *
     * @param branch the declared terminal branch, never {@code uncertain}
     * @param value the operation output, which may be {@code null}
     * @param receipt the completed receipt
     * @param failure the secret-safe failure, or {@code null}
     * @throws IllegalArgumentException if the branch or receipt is invalid
     */
    @JsonCreator
    public QueryResult(
            @JsonProperty(value = "branch", required = true) final String branch,
            @JsonProperty("value") final T value,
            @JsonProperty(value = "receipt", required = true) final Receipt receipt,
            @JsonProperty("failure") final Failure failure) {
        if (branch == null || branch.isEmpty() || ConnectorBranches.UNCERTAIN.equals(branch) || receipt == null) {
            throw new IllegalArgumentException("Query result branch is invalid");
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
        final QueryResult<?> that = (QueryResult<?>) other;
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
