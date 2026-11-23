// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.util.Objects;

/**
 * A committed provider position; every field belongs to the stored document contract.
 */
public final class PollCursorState {
    private final String token;
    private final KeyedSnapshot snapshot;
    private final long seededAtUnixMs;
    private final long committedAtUnixMs;
    private final long commitCount;
    private final long resetCount;
    private final Long lastResetAtUnixMs;

    private PollCursorState(final Builder builder) {
        this.token = builder.token;
        this.snapshot = builder.snapshot;
        this.seededAtUnixMs = builder.seededAtUnixMs;
        this.committedAtUnixMs = builder.committedAtUnixMs;
        this.commitCount = builder.commitCount;
        this.resetCount = builder.resetCount;
        this.lastResetAtUnixMs = builder.lastResetAtUnixMs;
    }

    /**
     * Starts a state with every field at its zero value.
     *
     * @return a new builder
     */
    public static Builder newBuilder() {
        return new Builder();
    }

    /**
     * Returns the connector's opaque encoded position, at most 64 KiB of UTF-8.
     *
     * @return the token, possibly empty
     */
    public String getToken() {
        return token;
    }

    /**
     * Returns the keyed snapshot committed atomically with the token.
     *
     * @return the snapshot, or {@code null} when unused
     */
    public KeyedSnapshot getSnapshot() {
        return snapshot;
    }

    /**
     * Returns the time of the latest seed in milliseconds since the Unix epoch.
     *
     * @return the seed time
     */
    public long getSeededAtUnixMs() {
        return seededAtUnixMs;
    }

    /**
     * Returns the time of this commit in milliseconds since the Unix epoch.
     *
     * @return the commit time
     */
    public long getCommittedAtUnixMs() {
        return committedAtUnixMs;
    }

    /**
     * Returns the count of every commit, seeds included.
     *
     * @return the commit count
     */
    public long getCommitCount() {
        return commitCount;
    }

    /**
     * Returns the count of reseeds after the provider rejected the cursor permanently.
     *
     * @return the reset count
     */
    public long getResetCount() {
        return resetCount;
    }

    /**
     * Returns the time of the latest reseed in milliseconds.
     *
     * @return the reseed time, or {@code null} before the first
     */
    public Long getLastResetAtUnixMs() {
        return lastResetAtUnixMs;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof PollCursorState)) {
            return false;
        }
        final PollCursorState that = (PollCursorState) other;
        return seededAtUnixMs == that.seededAtUnixMs && committedAtUnixMs == that.committedAtUnixMs
                && commitCount == that.commitCount && resetCount == that.resetCount && token.equals(that.token)
                && Objects.equals(snapshot, that.snapshot) && Objects.equals(lastResetAtUnixMs, that.lastResetAtUnixMs);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(token, snapshot, seededAtUnixMs, committedAtUnixMs, commitCount, resetCount,
                lastResetAtUnixMs);
    }

    /**
     * Builds a {@link PollCursorState}.
     */
    public static final class Builder {
        private String token = "";
        private KeyedSnapshot snapshot;
        private long seededAtUnixMs;
        private long committedAtUnixMs;
        private long commitCount;
        private long resetCount;
        private Long lastResetAtUnixMs;

        private Builder() {
        }

        /**
         * Sets the opaque position.
         *
         * @param value the token
         * @return this builder
         */
        public Builder token(final String value) {
            this.token = value == null ? "" : value;
            return this;
        }

        /**
         * Sets the keyed snapshot.
         *
         * @param value the snapshot, or {@code null}
         * @return this builder
         */
        public Builder snapshot(final KeyedSnapshot value) {
            this.snapshot = value;
            return this;
        }

        /**
         * Sets the seed time.
         *
         * @param value milliseconds since the Unix epoch
         * @return this builder
         */
        public Builder seededAtUnixMs(final long value) {
            this.seededAtUnixMs = value;
            return this;
        }

        /**
         * Sets the commit time.
         *
         * @param value milliseconds since the Unix epoch
         * @return this builder
         */
        public Builder committedAtUnixMs(final long value) {
            this.committedAtUnixMs = value;
            return this;
        }

        /**
         * Sets the commit count.
         *
         * @param value the nonnegative count
         * @return this builder
         */
        public Builder commitCount(final long value) {
            this.commitCount = value;
            return this;
        }

        /**
         * Sets the reset count.
         *
         * @param value the nonnegative count
         * @return this builder
         */
        public Builder resetCount(final long value) {
            this.resetCount = value;
            return this;
        }

        /**
         * Sets the latest reseed time.
         *
         * @param value milliseconds since the Unix epoch, or {@code null}
         * @return this builder
         */
        public Builder lastResetAtUnixMs(final Long value) {
            this.lastResetAtUnixMs = value;
            return this;
        }

        /**
         * Builds the state without validating it.
         *
         * @return the state
         */
        public PollCursorState build() {
            return new PollCursorState(this);
        }
    }
}
