// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.databind.annotation.JsonSerialize;
import java.io.IOException;
import java.time.Instant;

/**
 * Connector-specific refresh decision and provider exchange, without storage.
 *
 * @param <C> the connector-specific credential type
 */
public interface CredentialRefreshDriver<C> {
    /**
     * Decides deterministically whether this credential needs a refresh.
     *
     * @param state the current material, expiry, and decision time
     * @return {@code true} when a refresh is required
     */
    boolean isRefreshRequired(State<C> state);

    /**
     * Whether the stored grant can be submitted again after a completed exchange fails.
     *
     * <p>The default protects credentials that might rotate during dispatch. Return true only
     * when every exchange this state selects leaves the submitted credentials reusable.
     * Explicit reauthorization errors and unfinished driver calls retain their normal handling.
     *
     * @param state the credential state used for the failed exchange
     * @return whether a later call may use that same grant again
     */
    default boolean canRepeatFailedExchange(final State<C> state) {
        return false;
    }

    /**
     * Exchanges credentials; callers persist the complete result atomically.
     *
     * @param state the current material, expiry, and decision time
     * @return the complete replacement material
     * @throws IOException if the provider exchange fails
     * @throws InterruptedException if the exchange is interrupted
     */
    Result<C> refresh(State<C> state) throws IOException, InterruptedException;

    /**
     * Current credential material, optional expiry, and decision time.
     *
     * @param <C> the connector-specific credential type
     */
    @JsonSerialize(using = PrivateValueRefusingSerializer.class)
    final class State<C> {
        private final C credentials;
        private final Instant expiresAt;
        private final Instant now;

        /**
         * Creates a refresh state.
         *
         * @param credentials the current material
         * @param expiresAt the expiry, or {@code null} when unknown
         * @param now the decision time
         */
        public State(final C credentials, final Instant expiresAt, final Instant now) {
            this.credentials = credentials;
            this.expiresAt = expiresAt;
            this.now = now;
        }

        /**
         * Returns the current material.
         *
         * @return the credentials
         */
        public C getCredentials() {
            return credentials;
        }

        /**
         * Returns the expiry.
         *
         * @return the expiry, or {@code null} when unknown
         */
        public Instant getExpiresAt() {
            return expiresAt;
        }

        /**
         * Returns the decision time.
         *
         * @return the decision time
         */
        public Instant getNow() {
            return now;
        }

        @Override
        public String toString() {
            return "CredentialRefreshDriver.State{expiresAt=" + expiresAt + ", now=" + now + "}";
        }
    }

    /**
     * Complete replacement credential material and its required expiry.
     *
     * @param <C> the connector-specific credential type
     */
    @JsonSerialize(using = PrivateValueRefusingSerializer.class)
    final class Result<C> {
        private final C credentials;
        private final Instant expiresAt;

        /**
         * Creates a refresh result.
         *
         * @param credentials the complete replacement material
         * @param expiresAt the required expiry
         */
        public Result(final C credentials, final Instant expiresAt) {
            if (credentials == null || expiresAt == null) {
                throw new IllegalArgumentException("refreshed credentials and expiry are required");
            }
            this.credentials = credentials;
            this.expiresAt = expiresAt;
        }

        /**
         * Returns the replacement material.
         *
         * @return the credentials
         */
        public C getCredentials() {
            return credentials;
        }

        /**
         * Returns the required expiry.
         *
         * @return the expiry
         */
        public Instant getExpiresAt() {
            return expiresAt;
        }

        @Override
        public String toString() {
            return "CredentialRefreshDriver.Result{expiresAt=" + expiresAt + "}";
        }
    }
}
