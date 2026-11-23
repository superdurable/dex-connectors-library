// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

/**
 * Read-only provider operation invoked once per Dex Execute attempt.
 *
 * <p>Implementations classify a definite business outcome as a declared branch and a
 * safe-to-repeat failure as a retry. They must not retain credentials in results. Every Query is
 * safe to repeat.
 *
 * @param <I> the operation input type
 * @param <O> the operation output type
 */
public interface Query<I, O> {
    /**
     * Returns the immutable operation identity, branches, and Step defaults.
     *
     * @return the Query definition
     */
    QueryDefinition definition();

    /**
     * Performs one provider call and classifies its attempt.
     *
     * @param call the call identity and Dex Context
     * @param input the operation input
     * @return the classified attempt
     * @throws InterruptedException if Dex cancels the attempt while it waits
     */
    QueryAttempt<O> invoke(Call call, I input) throws InterruptedException;
}
