// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import io.superdurable.dexconnectors.sdk.trigger.TriggerCancellation;
import io.superdurable.dexconnectors.sdk.trigger.TriggerContext;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDelivery;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDeliveryAbortedException;
import io.superdurable.dexconnectors.sdk.trigger.TriggerLogLevel;
import io.superdurable.dexconnectors.sdk.trigger.TriggerRunner;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorCompletionService;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

/**
 * Serves one endpoint and runs the Trigger runners of its bindings together.
 *
 * <p>{@link #start()} runs the runners on a background thread. When they stop or fail before
 * {@link #close()}, it logs the failure, the {@link WebhookHttpHandler} answers 503, and it
 * restarts them after a backoff of 250 milliseconds doubling to 30 seconds;
 * {@link #getRunFailure()} reports the latest failure for a readiness probe. A host that owns its
 * threads calls {@link #run(TriggerContext)} itself and stops it by cancelling the context.
 */
public final class WebhookEndpointRunner implements AutoCloseable {
    private static final Duration RUN_RESTART_RESET = Duration.ofSeconds(30);

    private final WebhookEndpoint<?, ?> endpoint;
    private final List<TriggerRunner> runners;
    private final TriggerContext context = new TriggerContext();
    private volatile Exception runFailure;
    private Thread runThread;

    /**
     * Binds the endpoint and at least one runner.
     *
     * @param endpoint the webhook endpoint
     * @param runners the bindings' Trigger runners
     * @throws IllegalArgumentException without an endpoint or runners
     */
    public WebhookEndpointRunner(final WebhookEndpoint<?, ?> endpoint, final List<TriggerRunner> runners) {
        if (endpoint == null || runners == null || runners.isEmpty() || runners.contains(null)) {
            throw new IllegalArgumentException(
                    "webhook endpoint runner requires an endpoint and at least one Trigger runner");
        }
        this.endpoint = endpoint;
        this.runners = Collections.unmodifiableList(new ArrayList<>(runners));
    }

    /**
     * Returns the failure that stopped the started runners.
     *
     * <p>It is set from the failure until the next restart attempt begins; a host-owned
     * {@link #run} never sets it.
     *
     * @return the failure, or {@code null} while the runners run
     */
    public Exception getRunFailure() {
        return runFailure;
    }

    /**
     * Answers one request through the endpoint; see {@link WebhookEndpoint#handleRequest}.
     *
     * @param method the HTTP method
     * @param url the raw request target
     * @param headers the header pairs
     * @param body the complete raw body
     * @param cancellation fires when the request is abandoned, or {@code null}
     * @return the response
     * @throws InterruptedException if the request thread is interrupted
     */
    public WebhookResponse handleRequest(
            final String method, final String url, final Iterable<? extends Map.Entry<String, String>> headers,
            final byte[] body, final TriggerCancellation cancellation) throws InterruptedException {
        return endpoint.handleRequest(method, url, headers, body, cancellation);
    }

    /**
     * Starts the runners on a background thread that restarts them after a failure.
     *
     * @throws IllegalStateException when already started
     */
    public synchronized void start() {
        if (runThread != null) {
            throw new IllegalStateException("webhook endpoint runner is already started");
        }
        runThread = new Thread(this::runUntilClosed, "dex-connector-webhook-runner");
        runThread.setDaemon(true);
        runThread.start();
    }

    /**
     * Stops the started runners and waits for them to finish.
     *
     * <p>An interrupted wait returns early and restores the thread's interrupt status.
     */
    @Override
    public void close() {
        context.getCancellation().cancel();
        final Thread thread;
        synchronized (this) {
            thread = runThread;
        }
        if (thread != null) {
            try {
                thread.join();
            } catch (InterruptedException error) {
                Thread.currentThread().interrupt();
            }
        }
    }

    /**
     * Runs every Trigger runner until the context is cancelled or one of them stops.
     *
     * <p>When the first runner stops, the others are cancelled and awaited.
     *
     * @param parentContext the run context, or {@code null} for one that is never cancelled
     * @throws TriggerDeliveryAbortedException when the context was cancelled
     * @throws Exception the first runner's failure unless it is an abort
     */
    public void run(final TriggerContext parentContext) throws Exception {
        final TriggerContext parent = parentContext == null ? new TriggerContext() : parentContext;
        final TriggerCancellation childCancellation = new TriggerCancellation(parent.getCancellation());
        final TriggerContext child = parent.withCancellation(childCancellation);
        final ExecutorService executor = Executors.newFixedThreadPool(runners.size(), runnable -> {
            final Thread thread = new Thread(runnable, "dex-connector-trigger-runner");
            thread.setDaemon(true);
            return thread;
        });
        final ExecutorCompletionService<Void> completion = new ExecutorCompletionService<>(executor);
        final List<Future<Void>> tasks = new ArrayList<>();
        for (TriggerRunner runner : runners) {
            tasks.add(completion.submit(() -> {
                runner.run(child);
                return null;
            }));
        }
        Exception firstFailure = null;
        try {
            final Future<Void> first = completion.take();
            try {
                first.get();
            } catch (ExecutionException error) {
                firstFailure = error.getCause() instanceof Exception ? (Exception) error.getCause() : error;
            }
        } finally {
            childCancellation.cancel();
            executor.shutdown();
            for (Future<Void> task : tasks) {
                try {
                    task.get();
                } catch (ExecutionException ignored) {
                    // Only the first runner's failure is reported; the rest stopped because of it.
                }
            }
        }
        if (parent.getCancellation().isCancelled()) {
            throw new TriggerDeliveryAbortedException();
        }
        if (firstFailure != null && !TriggerDelivery.isTriggerDeliveryAborted(firstFailure)) {
            throw firstFailure;
        }
    }

    WebhookEndpoint<?, ?> endpoint() {
        return endpoint;
    }

    private void runUntilClosed() {
        int failedAttempts = 0;
        while (true) {
            final long startedAt = System.nanoTime();
            Exception failure;
            try {
                run(context);
                failure = new IllegalStateException("webhook Trigger runners stopped");
            } catch (TriggerDeliveryAbortedException error) {
                if (context.getCancellation().isCancelled()) {
                    return;
                }
                failure = new IllegalStateException("webhook Trigger runners stopped");
            } catch (InterruptedException error) {
                return;
            } catch (Exception error) {
                failure = error;
            }
            if (context.getCancellation().isCancelled()) {
                return;
            }
            runFailure = failure;
            TriggerDelivery.logTriggerRecord(endpoint.config().getLogger(), new TriggerContext(), TriggerLogLevel.ERROR,
                    "webhook endpoint runner failed", TriggerDelivery.triggerErrorAttributes(failure));
            // A run that outlived the longest backoff starts the backoff over.
            if (Duration.ofNanos(System.nanoTime() - startedAt).compareTo(RUN_RESTART_RESET) >= 0) {
                failedAttempts = 0;
            }
            failedAttempts++;
            try {
                TriggerDelivery.waitForTriggerDelay(context.getCancellation(),
                        TriggerDelivery.triggerRetryDelay(failedAttempts));
            } catch (TriggerDeliveryAbortedException | InterruptedException error) {
                return;
            }
            runFailure = null;
        }
    }
}
