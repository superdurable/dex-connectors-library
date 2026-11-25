// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

/**
 * The attempt has no remaining permission to send the provider request.
 */
public final class DispatchNotAllowedException extends IllegalStateException {
    private static final long serialVersionUID = 1L;

    DispatchNotAllowedException() {
        super("single dispatch is not allowed for this attempt");
    }
}
