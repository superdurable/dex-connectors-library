// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonValue;

/**
 * Manifest {@code execution.repeatOnUnknown} tier for an unknown provider outcome.
 */
public enum RepeatOnUnknown {
    /** Repeating is harmless; retry within the Step retry policy. */
    SAFE("safe"),
    /** A duplicate is acceptable; resend while possible sends stay below {@code maxSends}. */
    LIMITED("limited"),
    /** A duplicate does real harm; read back only and never resend. */
    UNSAFE("unsafe");

    private final String wireValue;

    RepeatOnUnknown(final String wireValue) {
        this.wireValue = wireValue;
    }

    /**
     * Returns the manifest value.
     *
     * @return {@code safe}, {@code limited}, or {@code unsafe}
     */
    @JsonValue
    public String getWireValue() {
        return wireValue;
    }
}
