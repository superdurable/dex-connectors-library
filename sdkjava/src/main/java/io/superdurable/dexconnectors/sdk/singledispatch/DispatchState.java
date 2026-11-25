// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

/**
 * Last known provider dispatch state for one Execute attempt.
 */
public enum DispatchState {
    /** The attempt proved it sent nothing. */
    NOT_SENT("notSent"),
    /** The attempt may have sent the request. */
    CLAIMED("claimed"),
    /** The provider proved it did not process the request. */
    CAN_RESEND("canResend");

    private final String wireValue;

    DispatchState(final String wireValue) {
        this.wireValue = wireValue;
    }

    /**
     * Returns the checkpoint wire value.
     *
     * @return {@code notSent}, {@code claimed}, or {@code canResend}
     */
    public String getWireValue() {
        return wireValue;
    }

    static DispatchState fromWireValue(final String value) {
        for (DispatchState state : values()) {
            if (state.wireValue.equals(value)) {
                return state;
            }
        }
        return null;
    }
}
