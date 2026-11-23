// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * Marks an event that no retry can deliver; the source consumes it.
 *
 * <p>Durable inboxes, Trigger runners, and {@link TriggerDelivery#deliverTrigger} consume the
 * event instead of retrying it. A Dex RPC handler throws this exact class to reject an event. The
 * Dex Java Worker reports a handler failure with its Java class name, so Java RPC targets recognize
 * this class name and the cross-language type {@link #ERROR_TYPE} that Python and TypeScript
 * handlers report; Python and TypeScript targets do not recognize a Java handler's marker.
 */
public class UndeliverableTriggerException extends RuntimeException {
    /** Prefix of every marker message; RPC targets recognize a handler's marker by it. */
    public static final String MESSAGE = "Trigger event is undeliverable";

    /** Error type Python and TypeScript Dex Workers report for an RPC handler's marker. */
    public static final String ERROR_TYPE = "ConnectorsSDK.UndeliverableTriggerError";

    private static final long serialVersionUID = 1L;

    private final String reason;

    /**
     * Creates a marker without a reason.
     */
    public UndeliverableTriggerException() {
        super(MESSAGE);
        this.reason = "";
    }

    /**
     * Creates a marker with a textual reason.
     *
     * @param reason the reason; its text follows the marker message
     */
    public UndeliverableTriggerException(final String reason) {
        super(message(reason));
        this.reason = reason == null ? "" : reason;
    }

    /**
     * Creates a marker caused by another failure.
     *
     * @param cause the failure; its message follows the marker message
     */
    public UndeliverableTriggerException(final Throwable cause) {
        super(message(cause == null ? null : textOf(cause)), cause);
        this.reason = cause == null ? "" : textOf(cause);
    }

    /**
     * Returns the reason text this event is undeliverable.
     *
     * @return the reason, or empty
     */
    public String getReason() {
        return reason;
    }

    static String message(final String reason) {
        return reason == null || reason.isEmpty() ? MESSAGE : MESSAGE + ": " + reason;
    }

    private static String textOf(final Throwable cause) {
        return cause.getMessage() == null ? cause.getClass().getSimpleName() : cause.getMessage();
    }
}
