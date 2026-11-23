// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import java.util.Map;

/**
 * Receives Trigger delivery records. Implementations must not throw.
 *
 * <p>Attribute values are safe identifiers, counts, enum values, or flags: {@link String},
 * {@link Number}, or {@link Boolean}. They never contain payloads or error messages.
 */
public interface TriggerLogger {
    /**
     * Reports whether records at a level are written, so callers can skip building them.
     *
     * @param level the record level
     * @return {@code true} when records at the level are written
     */
    boolean isEnabledFor(TriggerLogLevel level);

    /**
     * Writes one record.
     *
     * @param level the record level
     * @param message the fixed record message
     * @param attributes the safe record attributes
     */
    void log(TriggerLogLevel level, String message, Map<String, Object> attributes);
}
