// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import io.superdurable.dexconnectors.sdk.ConnectorBranches;
import io.superdurable.dexconnectors.sdk.Failure;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * One scripted provider outcome: a branch, a retry, an uncertain write, or a listing page set.
 *
 * <p>Instances are immutable and may be reused by several scripts.
 *
 * @param <O> the operation output type
 */
public final class MockCase<O> {
    /** The category of a scripted outcome. */
    public enum Kind {
        /** A definite declared branch. */
        BRANCH,
        /** A retryable failure. */
        RETRY,
        /** An unconfirmable, possibly accepted Mutation. */
        UNCERTAIN,
        /** Successive listing pages answered by one-based page number. */
        PAGED
    }

    private final Kind kind;
    private final String branch;
    private final O value;
    private final Failure failure;
    private final Duration retryAfter;
    private final List<O> pages;

    private MockCase(
            final Kind kind, final String branch, final O value, final Failure failure, final Duration retryAfter,
            final List<O> pages) {
        this.kind = kind;
        this.branch = branch;
        this.value = value;
        this.failure = failure;
        this.retryAfter = retryAfter;
        this.pages = pages;
    }

    /**
     * Answers a definite declared branch without a failure.
     *
     * @param branch the declared branch
     * @param value the operation output
     * @param <O> the operation output type
     * @return the case
     */
    public static <O> MockCase<O> branch(final String branch, final O value) {
        return branch(branch, value, null);
    }

    /**
     * Answers a definite declared branch.
     *
     * @param branch the declared branch
     * @param value the operation output
     * @param failure the secret-safe failure of a non-happy branch, or {@code null}
     * @param <O> the operation output type
     * @return the case
     */
    public static <O> MockCase<O> branch(final String branch, final O value, final Failure failure) {
        return new MockCase<O>(Kind.BRANCH, branch, value, failure, Duration.ZERO, Collections.<O>emptyList());
    }

    /**
     * Requests another Step attempt using the Step retry policy backoff.
     *
     * @param failure the secret-safe failure
     * @param <O> the operation output type
     * @return the case
     */
    public static <O> MockCase<O> retry(final Failure failure) {
        return retry(failure, Duration.ZERO);
    }

    /**
     * Requests another Step attempt after a provider delay.
     *
     * @param failure the secret-safe failure
     * @param retryAfter the nonnegative delay
     * @param <O> the operation output type
     * @return the case
     */
    public static <O> MockCase<O> retry(final Failure failure, final Duration retryAfter) {
        return new MockCase<O>(Kind.RETRY, "", null, failure, retryAfter, Collections.<O>emptyList());
    }

    /**
     * Answers a nonrepeatable, unconfirmable, possibly accepted Mutation.
     *
     * @param value the operation output
     * @param failure the secret-safe reason the outcome is unknown
     * @param <O> the operation output type
     * @return the case
     */
    public static <O> MockCase<O> uncertain(final O value, final Failure failure) {
        return new MockCase<O>(Kind.UNCERTAIN, ConnectorBranches.UNCERTAIN, value, failure, Duration.ZERO,
                Collections.<O>emptyList());
    }

    /**
     * Answers successive pages of a listing by one-based page number.
     *
     * @param branch the declared branch of every page
     * @param pages the pages in order
     * @param <O> the operation output type
     * @return the case
     * @throws IllegalArgumentException if no page is given
     */
    @SafeVarargs
    public static <O> MockCase<O> paged(final String branch, final O... pages) {
        if (pages == null || pages.length == 0) {
            throw new IllegalArgumentException("paged mock requires at least one page");
        }
        final List<O> copied = new ArrayList<>();
        for (O page : pages) {
            copied.add(page);
        }
        return new MockCase<O>(Kind.PAGED, branch, null, null, Duration.ZERO, Collections.unmodifiableList(copied));
    }

    /**
     * Returns the outcome category.
     *
     * @return the kind
     */
    public Kind getKind() {
        return kind;
    }

    /**
     * Returns the scripted branch.
     *
     * @return the branch, or empty for a retry
     */
    public String getBranch() {
        return branch;
    }

    /**
     * Returns the scripted output.
     *
     * @return the value, or {@code null}
     */
    public O getValue() {
        return value;
    }

    /**
     * Returns the scripted failure.
     *
     * @return the failure, or {@code null}
     */
    public Failure getFailure() {
        return failure;
    }

    /**
     * Returns the scripted retry delay.
     *
     * @return the delay
     */
    public Duration getRetryAfter() {
        return retryAfter;
    }

    /**
     * Returns the scripted listing pages.
     *
     * @return the unmodifiable pages, empty unless paged
     */
    public List<O> getPages() {
        return pages;
    }
}
