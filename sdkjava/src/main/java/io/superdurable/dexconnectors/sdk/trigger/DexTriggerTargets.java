// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import io.superdurable.dex.Client;
import io.superdurable.dex.Flow;
import io.superdurable.dex.IdReusePolicy;
import io.superdurable.dex.StartFlowOptions;
import io.superdurable.dex.exceptions.FlowAlreadyStartedException;
import io.superdurable.dex.exceptions.FlowDefinitionException;
import io.superdurable.dex.exceptions.FlowNotActiveOrNotFoundException;
import io.superdurable.dex.exceptions.FlowNotFoundException;
import io.superdurable.dex.exceptions.ValueMappingException;
import io.superdurable.dex.exceptions.WorkerInvocationException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Arrays;
import java.util.Base64;
import java.util.Collections;
import java.util.Map;

/**
 * Trigger targets that start a Dex Flow or invoke a Dex RPC, and Dex error classification.
 */
public final class DexTriggerTargets {
    /** Prefix of every request ID that {@link #triggerRpcRequestId} derives. */
    public static final String TRIGGER_RPC_REQUEST_ID_PREFIX = "trigger-rpc-";

    private DexTriggerTargets() {
    }

    /**
     * Creates a filtered target that starts a Dex Flow.
     *
     * <p>The event ID is the Flow-start request ID and Flow ID reuse is disallowed, so a duplicate
     * start is delivered even after the original Flow completes. A blank Flow ID or event ID, an
     * input that cannot be encoded, or an unregistered Flow throws
     * {@link UndeliverableTriggerException}. Mapper errors and every other error propagate for a
     * retry. It logs INFO "trigger event skipped: filtered" and DEBUG "trigger event delivered"
     * with {@code duplicate}.
     *
     * @param client the Dex Client
     * @param flow the exact Flow instance registered with the client
     * @param acceptsEvent the event filter
     * @param resolveFlowId the Flow ID resolver
     * @param mapToFlowInput the Flow start input mapper
     * @param logger the record destination, or {@code null} for the default
     * @param <P> the payload type
     * @param <I> the Flow start input type
     * @return the target
     * @throws IllegalArgumentException when an argument is missing
     */
    public static <P, I> TriggerTarget<P> newDexFlowTriggerTarget(
            final Client client, final Flow<I> flow, final TriggerFilter<P> acceptsEvent,
            final FlowIdResolver<P> resolveFlowId, final TriggerInputMapper<P, I> mapToFlowInput,
            final TriggerLogger logger) {
        if (client == null || flow == null || acceptsEvent == null || resolveFlowId == null || mapToFlowInput == null) {
            throw new IllegalArgumentException(
                    "Dex client, Flow, Trigger filter, Flow ID resolver, and Flow input mapper are required");
        }
        final TriggerLogger boundLogger = TriggerDelivery.bindTriggerLogAttributes(logger,
                Collections.<String, Object>singletonMap("target", "flow_start"));
        return (context, event) -> {
            if (!acceptsEvent.acceptsEvent(event)) {
                TriggerDelivery.logTriggerRecord(boundLogger, context, TriggerLogLevel.INFO,
                        "trigger event skipped: filtered", TriggerDelivery.eventAttributes(event));
                return;
            }
            final String flowId = resolveFlowId.resolveFlowId(event);
            if (isBlank(flowId) || isBlank(event.getId())) {
                throw new UndeliverableTriggerException(
                        new IllegalArgumentException("Flow trigger requires Flow ID and event ID"));
            }
            final I flowInput = mapToFlowInput.mapToInput(event);
            final StartFlowOptions options = StartFlowOptions.newBuilder()
                    .idReusePolicy(IdReusePolicy.DISALLOW)
                    .requestId(event.getId())
                    .build();
            boolean isDuplicate = false;
            try {
                client.startFlow(flow, flowId, flowInput, options);
            } catch (FlowAlreadyStartedException error) {
                isDuplicate = true;
            } catch (FlowDefinitionException error) {
                throw TriggerDelivery.markTriggerUndeliverable(error);
            } catch (RuntimeException error) {
                throw classifyDexTriggerError(error);
            }
            final Map<String, Object> attributes = TriggerDelivery.eventAttributes(event);
            attributes.put("flow_id", flowId);
            attributes.put("duplicate", isDuplicate);
            TriggerDelivery.logTriggerRecord(boundLogger, context, TriggerLogLevel.DEBUG, "trigger event delivered",
                    attributes);
        };
    }

    /**
     * Creates a filtered target that invokes one RPC on the resolved Flow.
     *
     * <p>The target creates a stub with {@link Client#newRpcStub(Class, String)} for the resolved
     * Flow ID and passes it with the mapped input to {@code invocation}. Without a mapper the input
     * is {@code null}.
     *
     * <p>Dex Java SDK 1.45.2 cannot send a request ID with an RPC, so unlike the Go, Python, and
     * TypeScript targets this target cannot ask Dex Server to apply the RPC at most once per event:
     * a durable inbox replay, a provider retry, or a retry after a timeout that hid an applied RPC
     * runs the handler again. Make the handler idempotent by event, for example by recording handled
     * event IDs in an Attribute map. It stays package-private until the Dex Java Client can send an
     * RPC request ID, so applications cannot depend on the weaker at-least-once behavior.
     *
     * <p>It throws {@link UndeliverableTriggerException} when the Flow was never started or has
     * closed, when the handler throws {@link UndeliverableTriggerException}, when a value cannot be
     * mapped, or when the Flow ID or event ID is blank. Every other error propagates for a retry.
     *
     * @param client the Dex Client
     * @param flowClass the registered Flow class that declares the RPC
     * @param invocation the RPC invocation through the stub
     * @param acceptsEvent the event filter
     * @param resolveFlowId the Flow ID resolver
     * @param mapToRpcInput the RPC input mapper, or {@code null} for an RPC without input
     * @param logger the record destination, or {@code null} for the default
     * @param <P> the payload type
     * @param <F> the Flow class
     * @param <I> the RPC input type
     * @return the target
     * @throws IllegalArgumentException when a required argument is missing
     */
    static <P, F, I> TriggerTarget<P> newDexRpcTriggerTarget(
            final Client client, final Class<F> flowClass, final DexRpcInvocation<F, I> invocation,
            final TriggerFilter<P> acceptsEvent, final FlowIdResolver<P> resolveFlowId,
            final TriggerInputMapper<P, I> mapToRpcInput, final TriggerLogger logger) {
        if (client == null || flowClass == null || invocation == null || acceptsEvent == null || resolveFlowId == null) {
            throw new IllegalArgumentException("Dex client, RPC, Trigger filter, and Flow ID resolver are required");
        }
        final TriggerLogger boundLogger = TriggerDelivery.bindTriggerLogAttributes(logger,
                Collections.<String, Object>singletonMap("target", "rpc"));
        return (context, event) -> {
            if (!acceptsEvent.acceptsEvent(event)) {
                TriggerDelivery.logTriggerRecord(boundLogger, context, TriggerLogLevel.INFO,
                        "trigger event skipped: filtered", TriggerDelivery.eventAttributes(event));
                return;
            }
            final String flowId = resolveFlowId.resolveFlowId(event);
            if (isBlank(flowId) || isBlank(event.getId())) {
                throw new UndeliverableTriggerException(
                        new IllegalArgumentException("RPC trigger requires Flow ID and event ID"));
            }
            final I input = mapToRpcInput == null ? null : mapToRpcInput.mapToInput(event);
            try {
                invocation.invokeRpc(client, client.newRpcStub(flowClass, flowId), input);
            } catch (InterruptedException error) {
                throw error;
            } catch (Exception error) {
                throw classifyDexTriggerError(error);
            }
            final Map<String, Object> attributes = TriggerDelivery.eventAttributes(event);
            attributes.put("flow_id", flowId);
            TriggerDelivery.logTriggerRecord(boundLogger, context, TriggerLogLevel.DEBUG, "trigger event delivered",
                    attributes);
        };
    }

    /**
     * Derives the Dex request ID the Go, Python, and TypeScript RPC targets send for one event.
     *
     * <p>The ID is {@link #TRIGGER_RPC_REQUEST_ID_PREFIX} followed by the first 16 bytes, as
     * unpadded base64url, of the SHA-256 digest of the UTF-8 bytes of {@code rpcName}, one 0x00
     * byte, and the UTF-8 bytes of {@code eventId}. An unpaired surrogate counts as U+FFFD. For
     * example, {@code triggerRpcRequestId("receiveReply", "Ev1")} returns
     * {@code "trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA"}.
     *
     * @param rpcName the RPC name
     * @param eventId the Trigger event ID
     * @return the request ID
     * @throws IllegalArgumentException when {@code rpcName} is empty or contains NUL
     */
    public static String triggerRpcRequestId(final String rpcName, final String eventId) {
        if (rpcName == null || rpcName.isEmpty() || rpcName.indexOf('\0') >= 0) {
            throw new IllegalArgumentException("RPC request ID requires an RPC name without NUL characters");
        }
        final MessageDigest digest;
        try {
            digest = MessageDigest.getInstance("SHA-256");
        } catch (NoSuchAlgorithmException error) {
            throw new IllegalStateException("SHA-256 is required by every Java platform", error);
        }
        digest.update(wellFormedUtf8(rpcName));
        digest.update((byte) 0);
        digest.update(wellFormedUtf8(eventId == null ? "" : eventId));
        return TRIGGER_RPC_REQUEST_ID_PREFIX
                + Base64.getUrlEncoder().withoutPadding().encodeToString(Arrays.copyOf(digest.digest(), 16));
    }

    /**
     * Marks the Dex errors no retry of the same event can fix; returns every other error unchanged.
     *
     * <p>A closed or never-started Flow, an RPC handler's own undeliverable marker, and a value that
     * cannot be mapped are marked. The Dex Java SDK does not say whether a
     * {@link ValueMappingException} came from encoding the input or decoding the response, so both
     * are marked; either way the event is consumed. A Worker code alone never qualifies.
     *
     * @param error the failure
     * @return the marker or the unchanged error
     */
    public static Exception classifyDexTriggerError(final Exception error) {
        if (TriggerDelivery.isTriggerUndeliverable(error)) {
            return error;
        }
        if (error instanceof FlowNotActiveOrNotFoundException || error instanceof FlowNotFoundException) {
            return TriggerDelivery.markTriggerUndeliverable(error);
        }
        if (error instanceof WorkerInvocationException) {
            return isHandlerUndeliverableMarker((WorkerInvocationException) error)
                    ? TriggerDelivery.markTriggerUndeliverable(error)
                    : error;
        }
        if (error instanceof ValueMappingException) {
            return TriggerDelivery.markTriggerUndeliverable(error);
        }
        return error;
    }

    private static boolean isHandlerUndeliverableMarker(final WorkerInvocationException error) {
        final String type = error.getWorkerErrorType();
        final String detail = error.getWorkerErrorDetail() == null ? "" : error.getWorkerErrorDetail();
        final boolean isMarkerType = UndeliverableTriggerException.ERROR_TYPE.equals(type)
                || UndeliverableTriggerException.class.getName().equals(type);
        return isMarkerType && (detail.equals(UndeliverableTriggerException.MESSAGE)
                || detail.startsWith(UndeliverableTriggerException.MESSAGE + ": "));
    }

    private static byte[] wellFormedUtf8(final String text) {
        final StringBuilder builder = new StringBuilder(text.length());
        for (int index = 0; index < text.length(); index++) {
            final char character = text.charAt(index);
            if (Character.isHighSurrogate(character) && index + 1 < text.length()
                    && Character.isLowSurrogate(text.charAt(index + 1))) {
                builder.append(character).append(text.charAt(index + 1));
                index++;
            } else if (Character.isSurrogate(character)) {
                builder.append('�');
            } else {
                builder.append(character);
            }
        }
        return builder.toString().getBytes(StandardCharsets.UTF_8);
    }

    private static boolean isBlank(final String value) {
        return value == null || value.trim().isEmpty();
    }
}
