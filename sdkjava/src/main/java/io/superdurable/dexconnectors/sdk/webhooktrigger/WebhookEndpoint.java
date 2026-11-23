// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.Credentials;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.trigger.TriggerCancellation;
import io.superdurable.dexconnectors.sdk.trigger.TriggerContext;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDelivery;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDeliveryAbortedException;
import io.superdurable.dexconnectors.sdk.trigger.TriggerEvent;
import io.superdurable.dexconnectors.sdk.trigger.TriggerFilter;
import io.superdurable.dexconnectors.sdk.trigger.TriggerLogLevel;
import io.superdurable.dexconnectors.sdk.trigger.TriggerTarget;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;

/**
 * Verifies, decodes, and records one connection's webhook requests for one Trigger.
 *
 * <p>Only a verified, decoded event takes an arrival slot, so unauthorized requests never hold up
 * valid ones. Events are recorded one at a time in arrival order: each running source that accepts
 * an event prepares it, such as a durable inbox write, and then queues it, before the request is
 * answered 200. A slot expires after the arrival timeout from its reservation or when the request
 * is abandoned; the request then answers 503 even while it still waits behind an earlier one. A
 * write already in flight at expiry still completes and queues the event, so delivery is at least
 * once.
 *
 * <p>Responses are {@code text/plain; charset=utf-8} and end with a newline: 405 for a method other
 * than POST, 413 for a body over the limit, 503 when credentials cannot be resolved or
 * verification is temporarily unavailable, 400 for a failed verification or an undecodable event,
 * 503 when the event cannot be recorded within the arrival timeout, and 200 once every accepting
 * binding recorded it. Serve it with {@link WebhookHttpHandler}. Instances are thread-safe.
 *
 * @param <C> the credential type
 * @param <P> the payload type
 */
public final class WebhookEndpoint<C, P> {
    private static final ThreadFactory DAEMON_THREADS = runnable -> {
        final Thread thread = new Thread(runnable, "dex-connector-webhook-recorder");
        thread.setDaemon(true);
        return thread;
    };

    private final WebhookEndpointConfig<C, P> config;
    private final Map<WebhookTriggerSource<P>, TriggerTarget<P>> runningSources = new LinkedHashMap<>();
    private final ExecutorService recorder = Executors.newSingleThreadExecutor(DAEMON_THREADS);
    private final ScheduledExecutorService slotTimers = Executors.newSingleThreadScheduledExecutor(DAEMON_THREADS);
    private int createdSourceCount;

    /**
     * Validates the configuration.
     *
     * @param config the endpoint configuration
     * @throws IllegalArgumentException for a blank identity, a missing callable, or an invalid bound
     */
    public WebhookEndpoint(final WebhookEndpointConfig<C, P> config) {
        if (config == null || isBlank(config.getConnectorId()) || isBlank(config.getTriggerName())) {
            throw new IllegalArgumentException("webhook endpoint connector ID and Trigger name are required");
        }
        if (config.getConnection() == null || isBlank(config.getConnection().getName())) {
            throw new IllegalArgumentException("webhook endpoint connection name is required");
        }
        if (config.getCredentials() == null || config.getDecodeEvent() == null || config.getVerifyRequest() == null) {
            throw new IllegalArgumentException(
                    "webhook endpoint credentials, decodeEvent, and verifyRequest are required");
        }
        if (config.getMaximumBodyBytes() <= 0) {
            throw new IllegalArgumentException("webhook endpoint maximumBodyBytes must be a positive integer");
        }
        if (config.getSourceQueueCapacity() <= 0) {
            throw new IllegalArgumentException("webhook endpoint sourceQueueCapacity must be a positive integer");
        }
        if (config.getArrivalTimeout() == null || config.getArrivalTimeout().isZero()
                || config.getArrivalTimeout().isNegative()) {
            throw new IllegalArgumentException("webhook endpoint arrivalTimeout must be positive");
        }
        if (config.getClock() == null) {
            throw new IllegalArgumentException("webhook endpoint clock is required");
        }
        this.config = config;
    }

    /**
     * Creates one binding's source.
     *
     * <p>The endpoint answers 503 to verified events until every created source is running, so a
     * binding still replaying its inbox never misses an event another binding acknowledged.
     *
     * @param acceptsEvent selects the binding's events, or {@code null} to accept every event
     * @return the source
     */
    public synchronized WebhookTriggerSource<P> newSource(final TriggerFilter<P> acceptsEvent) {
        createdSourceCount++;
        return new WebhookTriggerSource<P>(this, acceptsEvent == null ? event -> true : acceptsEvent);
    }

    /**
     * Returns how many sources are running.
     *
     * @return the running source count
     */
    public synchronized int runningSourceCount() {
        return runningSources.size();
    }

    /**
     * Answers one request with its complete body; server adapters call it.
     *
     * <p>Never pass a partial body: verification and decoding assume the complete bytes.
     *
     * @param method the HTTP method
     * @param url the raw request target
     * @param headers the header pairs in arrival order
     * @param body the complete raw body
     * @param cancellation fires when the request is abandoned, or {@code null}
     * @return the response
     * @throws InterruptedException if the request thread is interrupted
     */
    public WebhookResponse handleRequest(
            final String method, final String url, final Iterable<? extends Map.Entry<String, String>> headers,
            final byte[] body, final TriggerCancellation cancellation) throws InterruptedException {
        if (method == null || !"POST".equals(method.toUpperCase(Locale.ROOT))) {
            return WebhookResponse.text(405, "method not allowed", "allow", "POST");
        }
        if (body.length > config.getMaximumBodyBytes()) {
            return WebhookResponse.text(413, "request body too large", "connection", "close");
        }
        final TriggerCancellation requestCancellation = cancellation == null ? new TriggerCancellation() : cancellation;
        final WebhookRequest request = new WebhookRequest(url, headers, body, config.getClock().instant());
        final C credentials;
        try {
            credentials = resolveCredentials(body);
        } catch (InterruptedException error) {
            throw error;
        } catch (Exception error) {
            return WebhookResponse.text(503, "webhook temporarily unavailable");
        }
        try {
            config.getVerifyRequest().verifyRequest(request, credentials, requestCancellation);
        } catch (WebhookVerificationUnavailableException error) {
            return WebhookResponse.text(503, "webhook temporarily unavailable");
        } catch (InterruptedException error) {
            throw error;
        } catch (Exception error) {
            return WebhookResponse.text(400, "invalid webhook signature");
        }
        final TriggerEvent<P> event;
        try {
            event = config.getDecodeEvent().decodeEvent(request);
        } catch (InterruptedException error) {
            throw error;
        } catch (Exception error) {
            return WebhookResponse.text(400, "invalid webhook event");
        }
        if (event == null) {
            return WebhookResponse.empty(200);
        }
        try {
            recordInArrivalOrder(event, requestCancellation);
        } catch (InterruptedException error) {
            throw error;
        } catch (Exception error) {
            return WebhookResponse.text(503, "webhook delivery temporarily unavailable");
        }
        return WebhookResponse.empty(200);
    }

    WebhookEndpointConfig<C, P> config() {
        return config;
    }

    synchronized void activate(final WebhookTriggerSource<P> source, final TriggerTarget<P> target) {
        runningSources.put(source, target);
    }

    synchronized void deactivate(final WebhookTriggerSource<P> source) {
        runningSources.remove(source);
    }

    private C resolveCredentials(final byte[] body) throws Exception {
        final Call call = Call.newTriggerCall(config.getConnection(),
                new OperationRef(config.getConnectorId(), config.getTriggerName()),
                credentialCallId(config.getConnectorId(), config.getConnection(), body));
        if (config.getCredentialRefresh() != null) {
            return Credentials.resolveCredential(config.getCredentials(), call, config.getCredentialRefresh());
        }
        return config.getCredentials().resolve(call);
    }

    private void recordInArrivalOrder(final TriggerEvent<P> event, final TriggerCancellation requestCancellation)
            throws Exception {
        final TriggerCancellation slot = new TriggerCancellation(requestCancellation);
        final ScheduledFuture<?> timer = slotTimers.schedule(slot::cancel,
                config.getArrivalTimeout().toNanos(), TimeUnit.NANOSECONDS);
        final CountDownLatch isTurnOrSlotDone = new CountDownLatch(1);
        try {
            final Future<?> turn = recorder.submit(() -> {
                try {
                    // A turn whose slot expired while it waited skips recording.
                    slot.throwIfCancelled();
                    record(slot, event);
                    return null;
                } catch (TriggerDeliveryAbortedException error) {
                    throw error;
                } catch (Exception error) {
                    if (slot.isCancelled()) {
                        // The request already answered 503, so only a record shows the late failure.
                        logLateFailure(error);
                    }
                    throw error;
                } finally {
                    isTurnOrSlotDone.countDown();
                }
            });
            slot.whenCancelled(isTurnOrSlotDone::countDown);
            isTurnOrSlotDone.await();
            if (!turn.isDone()) {
                slot.cancel();
                throw new TriggerDeliveryAbortedException();
            }
            try {
                turn.get();
            } catch (ExecutionException error) {
                final Throwable cause = error.getCause();
                if (cause instanceof Exception) {
                    throw (Exception) cause;
                }
                throw error;
            }
        } finally {
            timer.cancel(false);
        }
    }

    private void record(final TriggerCancellation slot, final TriggerEvent<P> event) throws Exception {
        final List<Map.Entry<WebhookTriggerSource<P>, TriggerTarget<P>>> running;
        final int expectedSourceCount;
        synchronized (this) {
            running = new ArrayList<>(runningSources.entrySet());
            expectedSourceCount = createdSourceCount;
        }
        if (running.isEmpty()) {
            throw new IllegalStateException("no webhook Trigger source is running");
        }
        if (running.size() < expectedSourceCount) {
            throw new IllegalStateException("a webhook Trigger source is not running yet");
        }
        final List<WebhookTriggerSource<P>> accepting = new ArrayList<>();
        for (Map.Entry<WebhookTriggerSource<P>, TriggerTarget<P>> source : running) {
            if (!source.getKey().accepts(event)) {
                continue;
            }
            TriggerDelivery.prepareTriggerDelivery(new TriggerContext(slot), source.getValue(), event);
            accepting.add(source.getKey());
        }
        for (WebhookTriggerSource<P> source : accepting) {
            source.enqueue(event);
        }
    }

    void logLateFailure(final Exception error) {
        TriggerDelivery.logTriggerRecord(config.getLogger(), new TriggerContext(), TriggerLogLevel.DEBUG,
                "webhook late record failed", TriggerDelivery.triggerErrorAttributes(error));
    }

    static CallId credentialCallId(final String connectorId, final ConnectionRef connection, final byte[] body) {
        final MessageDigest digest;
        try {
            digest = MessageDigest.getInstance("SHA-256");
        } catch (NoSuchAlgorithmException error) {
            throw new IllegalStateException("SHA-256 is required by every Java platform", error);
        }
        for (String part : new String[] {connectorId, connection.getProvider(), connection.getName()}) {
            digest.update(part.getBytes(StandardCharsets.UTF_8));
            digest.update((byte) 0);
        }
        digest.update(body);
        final byte[] identity = digest.digest();
        identity[6] = (byte) ((identity[6] & 0x0f) | 0x50);
        identity[8] = (byte) ((identity[8] & 0x3f) | 0x80);
        final ByteBuffer buffer = ByteBuffer.wrap(identity, 0, 16);
        final long mostSignificantBits = buffer.getLong();
        return CallId.of(new UUID(mostSignificantBits, buffer.getLong()).toString());
    }

    private static boolean isBlank(final String value) {
        return value == null || value.trim().isEmpty();
    }
}
