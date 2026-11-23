// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

/**
 * Action permitted before credentials or provider lookups.
 */
public enum DecisionKind {
    /** Dispatch may send without reading back. */
    SEND_ALLOWED,
    /** Dispatch is forbidden; read back, or select {@code uncertain} when unconfirmed. */
    READ_BACK_REQUIRED,
    /** Dispatch is forbidden because the checkpoint could not be trusted. */
    FAIL_CLOSED,
    /**
     * Read back when possible; confirmed success selects the happy path.
     *
     * <p>A definite not-found or inconclusive read-back permits one Dispatch. A connector without
     * read-back may Dispatch directly. Returned only when bounded resend is enabled and the send
     * cap has not been reached.
     */
    READ_BACK_THEN_RESEND_ALLOWED
}
