// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/**
 * Structured best-effort Dex Stream message for one connector attempt.
 *
 * <p>Register a {@code Stream<ProgressUpdate>} in the consuming Flow and pass it to a Step
 * factory to receive progress. Streams are never the durable record.
 */
@JsonPropertyOrder({"callId", "attempt", "sequence", "phase", "message", "percent", "providerSequence"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class ProgressUpdate {
    private final CallId callId;
    private final int attempt;
    private final long sequence;
    private final String phase;
    private final String message;
    private final Double percent;
    private final Long providerSequence;

    /**
     * Creates a progress update.
     *
     * @param callId the Call ID
     * @param attempt the Dex Execute attempt
     * @param sequence the one-based sequence within the attempt
     * @param phase the stable phase name
     * @param message the secret-safe message, or empty
     * @param percent the completion fraction within 0..1, or {@code null}
     * @param providerSequence the provider-supplied sequence, or {@code null}
     */
    @JsonCreator
    public ProgressUpdate(
            @JsonProperty(value = "callId", required = true) final CallId callId,
            @JsonProperty(value = "attempt", required = true) final int attempt,
            @JsonProperty(value = "sequence", required = true) final long sequence,
            @JsonProperty(value = "phase", required = true) final String phase,
            @JsonProperty("message") final String message,
            @JsonProperty("percent") final Double percent,
            @JsonProperty("providerSequence") final Long providerSequence) {
        this.callId = callId;
        this.attempt = attempt;
        this.sequence = sequence;
        this.phase = phase;
        this.message = message == null ? "" : message;
        this.percent = percent;
        this.providerSequence = providerSequence;
    }

    /**
     * Returns the Call ID.
     *
     * @return the Call ID
     */
    @JsonProperty("callId")
    public CallId getCallId() {
        return callId;
    }

    /**
     * Returns the Dex Execute attempt.
     *
     * @return the attempt
     */
    @JsonProperty("attempt")
    public int getAttempt() {
        return attempt;
    }

    /**
     * Returns the one-based sequence within the attempt.
     *
     * @return the sequence
     */
    @JsonProperty("sequence")
    public long getSequence() {
        return sequence;
    }

    /**
     * Returns the stable phase name.
     *
     * @return the phase
     */
    @JsonProperty("phase")
    public String getPhase() {
        return phase;
    }

    /**
     * Returns the secret-safe message.
     *
     * @return the message, or empty
     */
    @JsonProperty("message")
    @JsonInclude(JsonInclude.Include.NON_EMPTY)
    public String getMessage() {
        return message;
    }

    /**
     * Returns the completion fraction.
     *
     * @return the fraction within 0..1, or {@code null}
     */
    @JsonProperty("percent")
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public Double getPercent() {
        return percent;
    }

    /**
     * Returns the provider-supplied sequence.
     *
     * @return the sequence, or {@code null}
     */
    @JsonProperty("providerSequence")
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public Long getProviderSequence() {
        return providerSequence;
    }
}
