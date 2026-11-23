// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * The Dex Web {@code connections.json} no longer lists the connection, so its credential must not
 * be used; reauthorize it in Dex Web or remove its use from the application.
 */
public final class ConnectionRemovedFromFileException extends IOException {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception with its fixed message.
     */
    public ConnectionRemovedFromFileException() {
        super("the connection is no longer in the connector configuration file");
    }
}
