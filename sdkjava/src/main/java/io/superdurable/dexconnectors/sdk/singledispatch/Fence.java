// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

import com.fasterxml.jackson.databind.JsonNode;
import io.superdurable.dex.Context;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConnectorSteps;
import java.io.IOException;
import java.time.Duration;
import java.time.Instant;
import java.util.Map;
import java.util.concurrent.Callable;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * Fences one non-idempotent provider send in a Dex Execute attempt.
 *
 * <p>Construct one Fence per logical Call and attempt on the Step handler thread. Call
 * {@link #decide()} before resolving credentials. {@link #dispatch} records the claim before
 * running the send, repeats it every five seconds while the request runs, and never runs the send
 * twice.
 *
 * <p>The send runs on a dedicated thread so that Dex heartbeats, which must come from the handler
 * thread, continue while it waits; it must not use the Dex Context. The write window is a share of
 * the Execute time left since the SDK first observed the attempt. A send that starts after the
 * window or after Dex cancels the attempt is refused. A send still running at the write deadline
 * is interrupted, I/O registered through {@link DispatchObservation#registerCancellation} is closed,
 * and {@link WriteWindowTimeoutException} is thrown; the SDK provider HTTP client also bounds its
 * connect and read timeouts by the window and checks {@link DispatchObservation#isActive()} around
 * connect. The handler thread polls Dex cancellation while it waits and cancels the send the same
 * way. A dispatch holds its Call ID in this process from before its claim until its send callback
 * returns: while it is held, {@link #decide()} of another attempt reads back instead of sending,
 * and {@link #dispatch} throws {@link DispatchNotAllowedException} even after an earlier decision. {@link #recordCanResend()} and
 * {@link #recordProviderId(String)} require that the send returned.
 *
 * <p>{@code maxSends} caps the possibly effective sends of one Call ID across all attempts. The
 * default 1 never resends after a possible send. A value from 2 to 10 enables bounded resend: a
 * {@link DecisionKind#READ_BACK_THEN_RESEND_ALLOWED} decision permits Dispatch after the
 * connector's read-back is not found or inconclusive, or directly when the connector cannot read
 * back. Pass the same cap, {@link Call#getMaxSends()}, on every attempt of a Step execution.
 *
 * <p>One Step may run Fences for several Call IDs in sequence; every heartbeat holds one marker
 * per Call ID the Step attempt knows. The heartbeat format is internal to the connector SDKs and
 * may change; hosts must not parse it.
 *
 * <pre>{@code
 * Fence fence = new Fence(call, Duration.ofSeconds(30), Fence.DEFAULT_WRITE_WINDOW_FRACTION, call.getMaxSends());
 * DispatchDecision decision = fence.decide();
 * if (decision.getKind() == DecisionKind.SEND_ALLOWED) {
 *     Message sent = fence.dispatch(writeDeadline -> client.send(message, writeDeadline));
 *     fence.recordProviderId(sent.getId());
 * }
 * }</pre>
 */
public final class Fence {
    /** Default fraction of the attempt budget available for the provider write. */
    public static final double DEFAULT_WRITE_WINDOW_FRACTION = 7.0 / 12.0;

    private static final Duration HEARTBEAT_INTERVAL = Duration.ofSeconds(5);
    private static final Duration CANCELLATION_POLL_INTERVAL = Duration.ofMillis(50);
    private static final Duration SEND_STOP_GRACE = Duration.ofSeconds(1);
    private static final Map<String, SendOccupancy> RUNNING_SENDS = new ConcurrentHashMap<>();
    private static final double MAXIMUM_WRITE_WINDOW_FRACTION = 5.0 / 6.0;

    private final Call call;
    private final Context context;
    private final int maxSends;
    private final StepCheckpoint checkpoint;
    private final Instant writeDeadline;
    private final Duration minimumRequestTime;
    private DispatchDecision decision;
    private boolean hasDispatched;
    private boolean hasSendFinished;
    private boolean hasPreSendRetry;
    private Marker marker;
    private SendOccupancy occupancy;

    /**
     * Creates a Fence with the default write window and a send cap of one.
     *
     * @param call the Call bound to a Dex Step Context
     * @param executeTimeout the Step Execute timeout
     */
    public Fence(final Call call, final Duration executeTimeout) {
        this(call, executeTimeout, DEFAULT_WRITE_WINDOW_FRACTION, SendCaps.MINIMUM_MAX_SENDS);
    }

    /**
     * Creates a Fence.
     *
     * @param call the Call bound to a Dex Step Context
     * @param executeTimeout the Step Execute timeout
     * @param writeWindowFraction the fraction of the timeout for the write, within (0, 5/6]
     * @param maxSends the send cap within 1..10, normally {@link Call#getMaxSends()}
     * @throws IllegalArgumentException if the Call has no Context, or a setting is invalid
     */
    public Fence(final Call call, final Duration executeTimeout, final double writeWindowFraction, final int maxSends) {
        if (call == null || call.getContext() == null || call.getId().isEmpty()
                || call.getContext().getAttempt() < 1 || executeTimeout == null
                || executeTimeout.isZero() || executeTimeout.isNegative()) {
            throw new IllegalArgumentException("single dispatch requires a call, positive attempt, and Execute timeout");
        }
        SendCaps.validateMaxSends(maxSends);
        if (Double.isNaN(writeWindowFraction) || writeWindowFraction <= 0
                || writeWindowFraction > MAXIMUM_WRITE_WINDOW_FRACTION) {
            throw new IllegalArgumentException("single-dispatch write window must be within (0, 5/6]");
        }
        this.call = call;
        this.context = call.getContext();
        this.maxSends = maxSends;
        this.checkpoint = StepCheckpoint.forContext(context);
        // Without a Dex attempt deadline (superdurable/dex#2175), subtract the attempt time already used.
        final Instant startedAt = Instant.now();
        final Duration used = Duration.between(ConnectorSteps.attemptObservedAt(context), startedAt);
        Duration budget = executeTimeout.minus(used.isNegative() ? Duration.ZERO : used);
        if (budget.isNegative()) {
            budget = Duration.ZERO;
        }
        final long budgetNanos = budget.toNanos();
        this.writeDeadline = startedAt.plusNanos((long) (budgetNanos * writeWindowFraction));
        this.minimumRequestTime = Duration.ofNanos(budgetNanos / 10);
    }

    /**
     * Returns the instant after which a running send is cancelled.
     *
     * @return the write deadline
     */
    public Instant getWriteDeadline() {
        return writeDeadline;
    }

    /**
     * Reads this call's prior checkpoint entry before any credential or provider lookup.
     *
     * <p>A heartbeat the Dex codec cannot map counts as no marker; any other read error fails
     * closed. Repeated calls return the first decision.
     *
     * @return the decision
     */
    public synchronized DispatchDecision decide() {
        if (decision != null) {
            return decision;
        }
        try {
            JsonNode previous = null;
            if (context.hasLastHeartbeatValue()) {
                try {
                    previous = context.getLastHeartbeatValue(JsonNode.class);
                } catch (io.superdurable.dex.exceptions.ValueMappingException error) {
                    previous = null;
                }
            }
            decision = DispatchDecision.decideDispatch(call.getId(), context.getAttempt(), previous, maxSends);
            if (isEarlierSendStillRunning(call.getId())
                    && (decision.getKind() == DecisionKind.SEND_ALLOWED
                    || decision.getKind() == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED)) {
                // A send of this call that could not be stopped may still reach the provider; never overlap it.
                decision = new DispatchDecision(DecisionKind.READ_BACK_REQUIRED, decision.getMarker(), "",
                        Math.max(decision.getMaximumPriorSends(), 1));
            }
        } catch (RuntimeException error) {
            decision = new DispatchDecision(DecisionKind.FAIL_CLOSED, null,
                    "single-dispatch checkpoint could not be read", 0);
        }
        return decision;
    }

    /**
     * Checkpoints a claim, runs the send once within the write window, and repeats heartbeats.
     *
     * <p>Allowed once after {@link DecisionKind#SEND_ALLOWED}, or after
     * {@link DecisionKind#READ_BACK_THEN_RESEND_ALLOWED} when read-back did not confirm the write.
     * The claim records one more send than the decision's maximum prior sends. A too-short write
     * window or failed claim records {@code notSent} only after {@code SEND_ALLOWED}.
     *
     * @param send the provider request, given the write deadline
     * @param <T> the send result type
     * @return the send result
     * @throws DispatchNotAllowedException if the decision does not permit a send
     * @throws InsufficientAttemptTimeException if the write window is too short; nothing was sent
     * @throws CheckpointException if the claim heartbeat failed; nothing was sent
     * @throws WriteWindowTimeoutException if the send passed the write deadline
     * @throws IOException if the send failed
     * @throws InterruptedException if Dex cancelled the attempt while the send ran
     */
    public <T> T dispatch(final ProviderSend<T> send) throws IOException, InterruptedException {
        final Marker claim;
        synchronized (this) {
            if (decision == null
                    || (decision.getKind() != DecisionKind.SEND_ALLOWED
                    && decision.getKind() != DecisionKind.READ_BACK_THEN_RESEND_ALLOWED)
                    || hasDispatched || hasPreSendRetry || send == null) {
                throw new DispatchNotAllowedException();
            }
            // notSent after an unresolved earlier send would let the next attempt skip read-back.
            final boolean shouldRecordNotSent = decision.getKind() == DecisionKind.SEND_ALLOWED;
            if (minimumRequestTime.isZero()
                    || Duration.between(Instant.now(), writeDeadline).compareTo(minimumRequestTime) < 0) {
                hasPreSendRetry = true;
                if (shouldRecordNotSent) {
                    try {
                        record(notSentMarker());
                    } catch (RuntimeException error) {
                        throw new InsufficientAttemptTimeException(
                                "single dispatch has insufficient time and checkpoint failed", error);
                    }
                }
                throw new InsufficientAttemptTimeException("insufficient attempt time for single dispatch", null);
            }
            // Registered before the claim, so no attempt of this call can overlap a send that is still alive.
            occupancy = new SendOccupancy(call.getId().value());
            if (RUNNING_SENDS.putIfAbsent(occupancy.callId, occupancy) != null) {
                occupancy = null;
                hasPreSendRetry = true;
                throw new DispatchNotAllowedException();
            }
            hasDispatched = true;
            claim = attemptMarker(DispatchState.CLAIMED, "", priorSendCount() + 1);
            try {
                record(claim);
            } catch (RuntimeException error) {
                occupancy.release();
                hasPreSendRetry = true;
                if (shouldRecordNotSent) {
                    try {
                        record(notSentMarker());
                    } catch (RuntimeException ignored) {
                        // The claim error is the one callers act on; nothing was sent.
                    }
                }
                throw new CheckpointException(error);
            }
            // The claim heartbeat may have taken the rest of the window; never start a late or cancelled send.
            if (!Instant.now().isBefore(writeDeadline) || context.isCancellationRequested()) {
                occupancy.release();
                hasPreSendRetry = true;
                RuntimeException notSentFailure = null;
                if (shouldRecordNotSent) {
                    try {
                        record(notSentMarker());
                    } catch (RuntimeException error) {
                        notSentFailure = error;
                    }
                }
                throw new InsufficientAttemptTimeException(
                        "single-dispatch write window ended before the send started", notSentFailure);
            }
        }
        final DispatchObservation observation = new DispatchObservation(writeDeadline);
        final SendOccupancy sendOccupancy = occupancy;
        final ExecutorService executor = Executors.newSingleThreadExecutor(runnable -> {
            final Thread thread = new Thread(runnable, "dex-connector-fenced-send");
            thread.setDaemon(true);
            return thread;
        });
        try {
            final Future<T> future = executor.submit(new Callable<T>() {
                @Override
                public T call() throws Exception {
                    if (!sendOccupancy.start()) {
                        throw new InterruptedException("single-dispatch send was cancelled before it started");
                    }
                    DispatchObservation.bind(observation);
                    try {
                        return send.send(writeDeadline);
                    } finally {
                        DispatchObservation.unbind();
                        sendOccupancy.release();
                    }
                }
            });
            return awaitSend(future, claim, observation);
        } finally {
            executor.shutdown();
        }
    }

    /**
     * Checkpoints a pre-send Retry and forbids Dispatch for this attempt.
     *
     * <p>Allowed only after {@link DecisionKind#SEND_ALLOWED}. After
     * {@link DecisionKind#READ_BACK_THEN_RESEND_ALLOWED} a possible earlier send is unresolved, so
     * return Retry without recording {@code notSent}.
     *
     * @throws DispatchNotAllowedException if the decision does not permit it
     */
    public synchronized void recordNotSent() {
        if (decision == null || decision.getKind() != DecisionKind.SEND_ALLOWED || hasDispatched || hasPreSendRetry) {
            throw new DispatchNotAllowedException();
        }
        hasPreSendRetry = true;
        record(notSentMarker());
    }

    /**
     * Checkpoints positive proof that the provider did not process the request.
     *
     * @throws DispatchNotAllowedException unless a send finished without a provider ID
     */
    public synchronized void recordCanResend() {
        if (!hasSendFinished || marker == null || marker.getState() != DispatchState.CLAIMED
                || !marker.getProviderId().isEmpty()) {
            throw new DispatchNotAllowedException();
        }
        record(attemptMarker(DispatchState.CAN_RESEND, "", priorSendCount()));
    }

    /**
     * Checkpoints an accepted resource identity for later read-back.
     *
     * <p>When the heartbeat fails, the error propagates, but the provider ID stays in this
     * attempt's checkpoint, so a later heartbeat of any Fence in the Step carries it.
     *
     * @param providerId the accepted provider resource ID
     * @throws DispatchNotAllowedException unless a send finished and the ID is nonempty
     */
    public synchronized void recordProviderId(final String providerId) {
        if (!hasSendFinished || marker == null || marker.getState() != DispatchState.CLAIMED
                || providerId == null || providerId.isEmpty()) {
            throw new DispatchNotAllowedException();
        }
        final Marker accepted = attemptMarker(DispatchState.CLAIMED, providerId, priorSendCount() + 1);
        try {
            checkpoint.record(context, accepted, true);
        } finally {
            marker = accepted;
        }
    }

    private <T> T awaitSend(final Future<T> future, final Marker claim, final DispatchObservation observation)
            throws IOException, InterruptedException {
        Instant nextHeartbeat = Instant.now().plus(HEARTBEAT_INTERVAL);
        while (true) {
            final Instant now = Instant.now();
            final long remainingNanos = Duration.between(now, writeDeadline).toNanos();
            if (remainingNanos <= 0) {
                // Close the provider I/O; the send may still have reached the provider, so no canResend.
                stopSend(future, observation);
                throw new WriteWindowTimeoutException(observation.statusCode());
            }
            // Dex cancellation reaches only the handler thread, so it is polled here and passed to the transport.
            if (context.isCancellationRequested()) {
                stopSend(future, observation);
                throw new InterruptedException("Dex cancelled the attempt during a single-dispatch send");
            }
            if (!now.isBefore(nextHeartbeat)) {
                repeatClaim(claim);
                nextHeartbeat = now.plus(HEARTBEAT_INTERVAL);
            }
            try {
                final T result = future.get(Math.min(remainingNanos, CANCELLATION_POLL_INTERVAL.toNanos()),
                        TimeUnit.NANOSECONDS);
                markSendFinished();
                return result;
            } catch (TimeoutException waiting) {
                // Keep polling for the deadline, Dex cancellation, and the heartbeat cadence.
            } catch (InterruptedException error) {
                stopSend(future, observation);
                throw error;
            } catch (ExecutionException error) {
                // The send thread returned with a failure, so it has stopped.
                markSendFinished();
                final Throwable cause = error.getCause();
                if (cause instanceof IOException) {
                    throw (IOException) cause;
                }
                if (cause instanceof InterruptedException) {
                    throw (InterruptedException) cause;
                }
                if (cause instanceof RuntimeException) {
                    throw (RuntimeException) cause;
                }
                if (cause instanceof Error) {
                    throw (Error) cause;
                }
                throw new IOException(cause);
            }
        }
    }

    /**
     * Aborts the send's I/O and interrupts it; a send that does not stop promptly blocks later
     * dispatches of the same call in this process until it ends.
     */
    private void stopSend(final Future<?> future, final DispatchObservation observation) {
        final SendOccupancy sendOccupancy = occupancy;
        if (sendOccupancy.preventStart()) {
            // The task never ran, so nothing was sent and nothing holds the call.
            future.cancel(false);
            sendOccupancy.release();
            return;
        }
        observation.cancel();
        future.cancel(true);
        boolean isInterrupted = false;
        final long deadline = System.nanoTime() + SEND_STOP_GRACE.toNanos();
        while (true) {
            final long remaining = deadline - System.nanoTime();
            try {
                if (remaining <= 0 || sendOccupancy.awaitRelease(remaining)) {
                    break;
                }
            } catch (InterruptedException interrupted) {
                // A second interrupt must not cut the grace wait short; it is restored afterwards.
                isInterrupted = true;
            }
        }
        if (isInterrupted) {
            Thread.currentThread().interrupt();
        }
    }

    private static boolean isEarlierSendStillRunning(final CallId callId) {
        return RUNNING_SENDS.containsKey(callId.value());
    }

    /** Holds one Call ID from before its claim until its send callback returns. */
    private static final class SendOccupancy {
        private final String callId;
        private final CountDownLatch released = new CountDownLatch(1);
        private final AtomicBoolean hasStartBeenDecided = new AtomicBoolean();

        private SendOccupancy(final String callId) {
            this.callId = callId;
        }

        boolean start() {
            return hasStartBeenDecided.compareAndSet(false, true);
        }

        boolean preventStart() {
            return hasStartBeenDecided.compareAndSet(false, true);
        }

        boolean awaitRelease(final long timeoutNanos) throws InterruptedException {
            return released.await(timeoutNanos, TimeUnit.NANOSECONDS);
        }

        void release() {
            RUNNING_SENDS.remove(callId, this);
            released.countDown();
        }
    }

    private synchronized void markSendFinished() {
        hasSendFinished = true;
    }

    private synchronized void repeatClaim(final Marker claim) {
        if (claim.equals(marker)) {
            try {
                checkpoint.record(context, claim, false);
            } catch (RuntimeException ignored) {
                // A missed repeat heartbeat only shortens the visible claim; the first claim stands.
            }
        }
    }

    private Marker notSentMarker() {
        return attemptMarker(DispatchState.NOT_SENT, "", priorSendCount());
    }

    private int priorSendCount() {
        return decision == null ? 0 : decision.getMaximumPriorSends();
    }

    private Marker attemptMarker(final DispatchState state, final String providerId, final int sendCount) {
        return new Marker(call.getId(), context.getAttempt(), state, providerId, sendCount);
    }

    private void record(final Marker value) {
        checkpoint.record(context, value, false);
        marker = value;
    }

    /**
     * Sends one provider request within the Fence write window.
     *
     * @param <T> the send result type
     */
    public interface ProviderSend<T> {
        /**
         * Sends the request; it must finish before the write deadline.
         *
         * @param writeDeadline the instant after which the send is cancelled
         * @return the send result
         * @throws IOException if the request failed
         * @throws InterruptedException if the send was cancelled
         */
        T send(Instant writeDeadline) throws IOException, InterruptedException;
    }
}
