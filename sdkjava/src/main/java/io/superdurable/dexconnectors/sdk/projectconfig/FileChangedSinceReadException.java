// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/** The Dex Web connections file changed since it was read; the caller reads the record again. */
final class FileChangedSinceReadException extends IOException {
    private static final long serialVersionUID = 1L;

    FileChangedSinceReadException() {
        super("the connector configuration file changed since it was read");
    }
}
