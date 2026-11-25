// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

/**
 * Provider-safe progress before Dex Call and attempt identity are added.
 */
public final class Progress {
    private final String phase;
    private final String message;
    private final Double percent;
    private final Long providerSequence;

    /**
     * Creates progress for one phase.
     *
     * @param phase the stable phase name
     * @param message the secret-safe message, or empty
     * @param percent the completion fraction within 0..1, or {@code null}
     * @param providerSequence the provider-supplied sequence, or {@code null}
     */
    public Progress(final String phase, final String message, final Double percent, final Long providerSequence) {
        this.phase = phase == null ? "" : phase;
        this.message = message == null ? "" : message;
        this.percent = percent;
        this.providerSequence = providerSequence;
    }

    /**
     * Creates progress with only a phase name.
     *
     * @param phase the stable phase name
     */
    public Progress(final String phase) {
        this(phase, "", null, null);
    }

    /**
     * Returns the stable phase name.
     *
     * @return the phase
     */
    public String getPhase() {
        return phase;
    }

    /**
     * Returns the secret-safe message.
     *
     * @return the message, or empty
     */
    public String getMessage() {
        return message;
    }

    /**
     * Returns the completion fraction.
     *
     * @return the fraction within 0..1, or {@code null}
     */
    public Double getPercent() {
        return percent;
    }

    /**
     * Returns the provider-supplied sequence.
     *
     * @return the sequence, or {@code null}
     */
    public Long getProviderSequence() {
        return providerSequence;
    }
}
