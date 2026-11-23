// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

/**
 * Identity of one durable poll cursor head: a Trigger binding's {@link PollCursorKey} or a shared
 * {@link PollFeedKey}.
 */
public interface PollCursorIdentity {
    /**
     * Returns the head object key in scope, as Go writes it.
     *
     * @param scope the project scope
     * @return the head key
     * @throws IllegalArgumentException if the identity is invalid
     */
    String path(Scope scope);
}
