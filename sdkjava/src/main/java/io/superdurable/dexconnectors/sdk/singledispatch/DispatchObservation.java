// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

import java.time.Instant;
import java.util.ArrayList;
import java.util.List;

/**
 * Connects provider I/O on a Fence send thread to the Fence that runs it.
 *
 * <p>Provider HTTP helpers call these methods from the thread that runs a Fence send; outside a
 * dispatch they are no-ops. The Fence reads the recorded HTTP status for a
 * {@link WriteWindowTimeoutException}, bounds every request by the write deadline, and closes
 * registered connections when the deadline passes, so blocked I/O ends instead of sending late.
 */
public final class DispatchObservation {
    private static final ThreadLocal<DispatchObservation> CURRENT = new ThreadLocal<>();

    private final Instant writeDeadline;
    private final List<Runnable> cancellations = new ArrayList<>();
    private volatile Integer statusCode;
    private volatile Thread sendThread;
    private boolean isCancelled;

    DispatchObservation(final Instant writeDeadline) {
        this.writeDeadline = writeDeadline;
    }

    /**
     * Records a response status for the enclosing Fence dispatch.
     *
     * @param statusCode the HTTP status code
     */
    public static void recordResponseStatus(final int statusCode) {
        final DispatchObservation observation = CURRENT.get();
        if (observation != null) {
            observation.statusCode = statusCode;
        }
    }

    /**
     * Forgets an earlier request's status as a new request starts.
     */
    public static void clearResponseStatus() {
        final DispatchObservation observation = CURRENT.get();
        if (observation != null) {
            observation.statusCode = null;
        }
    }

    /**
     * Returns the write deadline of the enclosing Fence dispatch.
     *
     * <p>Provider I/O must not outlive it: bound connect and read timeouts by the remaining time,
     * and refuse to start a request after it.
     *
     * @return the deadline, or {@code null} outside a dispatch
     */
    public static Instant currentWriteDeadline() {
        final DispatchObservation observation = CURRENT.get();
        return observation == null ? null : observation.writeDeadline;
    }

    /**
     * Registers an action that aborts in-flight I/O, such as closing a connection.
     *
     * <p>The Fence runs it when the write deadline passes or Dex cancels the attempt. When the
     * dispatch is already cancelled, the action runs immediately.
     *
     * @param cancel the abort action
     */
    public static void registerCancellation(final Runnable cancel) {
        final DispatchObservation observation = CURRENT.get();
        if (observation == null || cancel == null) {
            return;
        }
        final boolean shouldRunNow;
        synchronized (observation) {
            shouldRunNow = observation.isCancelled;
            if (!shouldRunNow) {
                observation.cancellations.add(cancel);
            }
        }
        if (shouldRunNow) {
            cancel.run();
        }
    }

    /**
     * Reports whether the enclosing Fence dispatch may still start provider I/O.
     *
     * <p>Call it right before each I/O step that could send bytes, such as connecting and writing
     * request headers or body. It is {@code false} once the write deadline passed or the Fence
     * cancelled the send because Dex cancelled the attempt.
     *
     * @return {@code true} outside a dispatch, or while the dispatch is active
     */
    public static boolean isActive() {
        final DispatchObservation observation = CURRENT.get();
        if (observation == null) {
            return true;
        }
        synchronized (observation) {
            if (observation.isCancelled) {
                return false;
            }
        }
        return Instant.now().isBefore(observation.writeDeadline);
    }

    static void bind(final DispatchObservation observation) {
        observation.sendThread = Thread.currentThread();
        CURRENT.set(observation);
    }

    Thread sendThread() {
        return sendThread;
    }

    static void unbind() {
        CURRENT.remove();
    }

    void cancel() {
        final List<Runnable> actions;
        synchronized (this) {
            isCancelled = true;
            actions = new ArrayList<>(cancellations);
            cancellations.clear();
        }
        for (Runnable action : actions) {
            try {
                action.run();
            } catch (RuntimeException ignored) {
                // Closing an already failed connection has nothing further to abort.
            }
        }
    }

    Integer statusCode() {
        return statusCode;
    }
}
