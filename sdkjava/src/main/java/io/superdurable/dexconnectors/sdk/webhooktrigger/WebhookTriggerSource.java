// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import io.superdurable.dexconnectors.sdk.trigger.TriggerContext;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDelivery;
import io.superdurable.dexconnectors.sdk.trigger.TriggerEvent;
import io.superdurable.dexconnectors.sdk.trigger.TriggerFilter;
import io.superdurable.dexconnectors.sdk.trigger.TriggerSource;
import io.superdurable.dexconnectors.sdk.trigger.TriggerTarget;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.concurrent.Semaphore;

/**
 * One binding's source of an endpoint's events; create it with {@link WebhookEndpoint#newSource}.
 *
 * <p>{@link #run} registers the source with its endpoint, delivers queued events in order with
 * {@link TriggerDelivery#deliverTrigger}, and throws
 * {@link io.superdurable.dexconnectors.sdk.trigger.TriggerDeliveryAbortedException} once the
 * context is cancelled. It runs at most once at a time.
 *
 * @param <P> the payload type
 */
public final class WebhookTriggerSource<P> implements TriggerSource<P> {
    private final WebhookEndpoint<?, P> endpoint;
    private final TriggerFilter<P> acceptsEvent;
    private final Deque<TriggerEvent<P>> queue = new ArrayDeque<>();
    private final Semaphore wakeups = new Semaphore(0);
    private boolean isRunning;

    WebhookTriggerSource(final WebhookEndpoint<?, P> endpoint, final TriggerFilter<P> acceptsEvent) {
        this.endpoint = endpoint;
        this.acceptsEvent = acceptsEvent;
    }

    boolean accepts(final TriggerEvent<P> event) {
        return acceptsEvent.acceptsEvent(event);
    }

    void enqueue(final TriggerEvent<P> event) {
        synchronized (queue) {
            if (queue.size() >= endpoint.config().getSourceQueueCapacity()) {
                throw new IllegalStateException("webhook Trigger delivery queue is full");
            }
            queue.addLast(event);
        }
        wakeups.release();
    }

    /**
     * Delivers this binding's events until the context is cancelled.
     *
     * @param context the run context
     * @param target the application target
     * @throws InterruptedException if the running thread is interrupted
     * @throws IllegalArgumentException without a target
     * @throws IllegalStateException when the source is already running
     */
    @Override
    public void run(final TriggerContext context, final TriggerTarget<P> target) throws InterruptedException {
        if (target == null) {
            throw new IllegalArgumentException("webhook Trigger target is required");
        }
        synchronized (this) {
            if (isRunning) {
                throw new IllegalStateException("webhook Trigger source is already running");
            }
            isRunning = true;
        }
        context.getCancellation().whenCancelled(wakeups::release);
        endpoint.activate(this, target);
        try {
            while (true) {
                context.getCancellation().throwIfCancelled();
                final TriggerEvent<P> event;
                synchronized (queue) {
                    event = queue.pollFirst();
                }
                if (event != null) {
                    TriggerDelivery.deliverTrigger(context, target, event, endpoint.config().getLogger());
                    continue;
                }
                wakeups.acquire();
            }
        } finally {
            endpoint.deactivate(this);
            synchronized (this) {
                isRunning = false;
            }
        }
    }
}
