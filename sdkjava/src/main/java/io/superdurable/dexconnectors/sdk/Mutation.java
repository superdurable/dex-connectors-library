// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

/**
 * Provider write with stable deduplication across Dex Execute retries.
 *
 * <p>An uncertain attempt is valid only when a request may have reached the provider and neither
 * safe repetition nor read-back can resolve the outcome.
 *
 * @param <I> the operation input type
 * @param <O> the operation output type
 */
public interface Mutation<I, O> {
    /**
     * Returns the immutable operation identity, branches, Step defaults, and resend tier.
     *
     * @return the Mutation definition
     */
    MutationDefinition definition();

    /**
     * Returns a provider-safe key that is stable for the logical call and input.
     *
     * @param callId the stable Call ID
     * @param input the operation input
     * @return the key, or empty to use the Call ID
     */
    String idempotencyKey(CallId callId, I input);

    /**
     * Performs one provider write and classifies its attempt.
     *
     * @param call the call identity, idempotency key, and Dex Context
     * @param input the operation input
     * @return the classified attempt
     * @throws InterruptedException if Dex cancels the attempt while it waits
     */
    MutationAttempt<O> invoke(Call call, I input) throws InterruptedException;
}
