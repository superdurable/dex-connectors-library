// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * Cooperative cancellation shared by a Trigger run or one webhook arrival slot.
 *
 * <p>Cancelling stops new work, such as a retry wait or a late inbox write, without interrupting
 * work already in flight. A child is cancelled when its parent is. Instances are thread-safe.
 */
public final class TriggerCancellation {
    private final CountDownLatch cancelled = new CountDownLatch(1);
    private final List<TriggerCancellation> children = new ArrayList<>();
    private final List<Runnable> cancellationActions = new ArrayList<>();

    /**
     * Creates a root cancellation.
     */
    public TriggerCancellation() {
    }

    /**
     * Creates a child, already cancelled when the parent is.
     *
     * @param parent the parent cancellation
     */
    public TriggerCancellation(final TriggerCancellation parent) {
        if (parent == null) {
            return;
        }
        synchronized (parent) {
            if (parent.isCancelled()) {
                cancelled.countDown();
            } else {
                parent.children.add(this);
            }
        }
    }

    /**
     * Cancels this and every child; calling it again has no effect.
     */
    public void cancel() {
        final List<TriggerCancellation> toCancel;
        final List<Runnable> actions;
        synchronized (this) {
            if (isCancelled()) {
                return;
            }
            cancelled.countDown();
            toCancel = new ArrayList<>(children);
            children.clear();
            actions = new ArrayList<>(cancellationActions);
            cancellationActions.clear();
        }
        for (Runnable action : actions) {
            action.run();
        }
        for (TriggerCancellation child : toCancel) {
            child.cancel();
        }
    }

    /**
     * Runs an action once cancellation is requested, or immediately when it already was.
     *
     * <p>The action runs on the cancelling thread; keep it short and non-blocking.
     *
     * @param action the action to run
     */
    public void whenCancelled(final Runnable action) {
        synchronized (this) {
            if (!isCancelled()) {
                cancellationActions.add(action);
                return;
            }
        }
        action.run();
    }

    /**
     * Reports whether cancellation was requested.
     *
     * @return {@code true} once cancelled
     */
    public boolean isCancelled() {
        return cancelled.getCount() == 0;
    }

    /**
     * Blocks until cancellation is requested.
     *
     * @throws InterruptedException if the waiting thread is interrupted
     */
    public void awaitCancellation() throws InterruptedException {
        cancelled.await();
    }

    /**
     * Blocks until cancellation is requested or the timeout elapses.
     *
     * @param timeout the longest wait
     * @return {@code true} when cancelled
     * @throws InterruptedException if the waiting thread is interrupted
     */
    public boolean awaitCancellation(final Duration timeout) throws InterruptedException {
        return cancelled.await(timeout.toNanos(), TimeUnit.NANOSECONDS);
    }

    /**
     * Throws when cancellation was requested.
     *
     * @throws TriggerDeliveryAbortedException once cancelled
     */
    public void throwIfCancelled() {
        if (isCancelled()) {
            throw new TriggerDeliveryAbortedException();
        }
    }
}
