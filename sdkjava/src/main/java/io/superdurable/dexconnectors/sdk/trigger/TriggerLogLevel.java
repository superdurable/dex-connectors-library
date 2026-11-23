// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * Severity of one Trigger delivery record.
 */
public enum TriggerLogLevel {
    /** Per-event delivery detail. */
    DEBUG,
    /** Replay progress, filtered events, and recovered deliveries. */
    INFO,
    /** Retried failures and undeliverable events. */
    WARN,
    /** Inbox read, write, and removal failures, and RPC request ID conflicts. */
    ERROR
}
