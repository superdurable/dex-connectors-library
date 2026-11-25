// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * A binding's backlog reached its bound; the source retries once the application consumes events.
 */
public final class TriggerInboxFullException extends IOException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception with its fixed message.
     */
    public TriggerInboxFullException() {
        super("project trigger inbox is full");
    }
}
