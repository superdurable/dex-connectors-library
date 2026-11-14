// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import {
  DexServiceError, FlowAlreadyStartedError, FlowNotActiveOrNotFoundError, FlowNotFoundError, IdReusePolicy, ValueMappingError, WorkerInvocationError, type Client, type Context, type Flow, type RPCInvokeOptions,
  type RPCResult,
} from '@superdurable/dex';
import { z } from 'zod';
import { ConnectionRefSchema, ConnectorConfigurationUISchema, type ConnectorConfigurationUI } from './types.js';
import {
  UndeliverableTriggerError, UndeliverableTriggerErrorType, UndeliverableTriggerMessage, isTriggerUndeliverable, logTriggerRecord, markTriggerUndeliverable, prepareTriggerDelivery, triggerErrorAttributes,
  withTriggerLoggerAttributes, type TriggerContext, type TriggerEvent, type TriggerLogger, type TriggerOptions, type TriggerTarget,
} from './trigger_delivery.js';

/** Stable manifest identity of one connector Trigger. */
export const TriggerRefSchema = z.strictObject({ connectorId: z.string().regex(/^[a-z][a-z0-9-]{1,62}$/), triggerName: z.string().regex(/^[a-z][A-Za-z0-9]+$/) });
/** Connector ID and lower camel case Trigger name. */
export type TriggerRef = z.infer<typeof TriggerRefSchema>;

/** One provider event source declared by a connector manifest. */
export const TriggerDefinitionSchema = z.strictObject({ trigger: TriggerRefSchema, description: z.string().refine(value => value.trim().length > 0, 'trigger description is required') });
/** Trigger identity and application-facing description. */
export type TriggerDefinition = z.infer<typeof TriggerDefinitionSchema>;

/** One configured use of a connector Trigger: its connection, Trigger, and stable binding name. */
export const TriggerBindingRefSchema = z.strictObject({ connection: ConnectionRefSchema, trigger: TriggerRefSchema, name: z.string().refine(value => value.trim().length > 0, 'trigger binding name is required') });
/** Configured Trigger binding identity. */
export type TriggerBindingRef = z.infer<typeof TriggerBindingRefSchema>;

/** One static application binding exposed to Flow Definition tooling and Dex Web. */
export const TriggerBindingDefinitionSchema = z.strictObject({
  definition: TriggerDefinitionSchema,
  connectionName: z.string().refine(value => value.trim().length > 0, 'trigger binding connection name is required'),
  bindingName: z.string().refine(value => value.trim().length > 0, 'trigger binding name is required'),
  configurationUI: ConnectorConfigurationUISchema.optional(),
});
/** Static Trigger binding metadata with optional Connector Studio configuration units. */
export interface TriggerBindingDefinition {
  /** Immutable Trigger definition. */ definition: TriggerDefinition;
  /** Configured provider connection name. */ connectionName: string;
  /** Configured Trigger binding name. */ bindingName: string;
  /** Optional Connector Studio configuration units. */ configurationUI?: ConnectorConfigurationUI;
}

/** Validate static Trigger binding metadata and return an isolated copy; throws on invalid wiring. */
export function mustTriggerBindingDefinition(definition: TriggerBindingDefinition): TriggerBindingDefinition {
  return TriggerBindingDefinitionSchema.parse(definition) as TriggerBindingDefinition;
}

/**
 * Owns the provider transport and emits acknowledged events.
 * run calls prepareTriggerDelivery before acknowledging an event, retries a retryable error (for example
 * with deliverTrigger or on its next poll), and treats an UndeliverableTriggerError as consumed rather
 * than stopping later deliveries. It resolves or rejects only when the context ends or it cannot recover.
 */
export interface TriggerSource<Payload> {
  /** Process provider events until the context ends or an unrecoverable error. */ run(context: TriggerContext, target: TriggerTarget<Payload>): Promise<void>;
}

/** A configured long-running connector Trigger. */
export interface TriggerRunner {
  /** Process provider events until the context ends or an unrecoverable error. */ run(context: TriggerContext): Promise<void>;
  /** Immutable connector Trigger definition. */ definition(): TriggerDefinition;
  /** Configured Trigger binding identity. */ binding(): TriggerBindingRef;
}

/** One provider source and application target. */
export interface TriggerConfig<Payload> {
  /** Immutable Trigger definition. */ definition: TriggerDefinition;
  /** Configured Trigger binding identity; its Trigger must equal the definition's. */ binding: TriggerBindingRef;
  /** Polls or subscribes to provider events. */ source: TriggerSource<Payload>;
  /** Routes accepted events into the application. */ target: TriggerTarget<Payload>;
  /** Receives the runner's records, such as an undeliverable event it consumes; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Validate and construct one Trigger runner.
 * run replays a replaying target first, then runs the source with a target that consumes
 * UndeliverableTriggerError results, logging each with the binding's identity, and forwards prepareTrigger.
 */
export function newTrigger<Payload>(config: TriggerConfig<Payload>): TriggerRunner {
  const definition = TriggerDefinitionSchema.parse(config.definition);
  const binding = TriggerBindingRefSchema.parse(config.binding);
  if (binding.trigger.connectorId !== definition.trigger.connectorId || binding.trigger.triggerName !== definition.trigger.triggerName) throw new TypeError('trigger binding does not match its definition');
  if (!config.source || !config.target) throw new TypeError('trigger source and target are required');
  const logger = withTriggerLoggerAttributes(config.logger, { connector: binding.trigger.connectorId, connection: binding.connection.name, trigger: binding.trigger.triggerName, binding: binding.name });
  const { source, target } = config;
  return {
    definition: () => structuredClone(definition),
    binding: () => structuredClone(binding),
    async run(context) {
      if (target.replayTriggerDeliveries) {
        try { await target.replayTriggerDeliveries(context); }
        catch (error: unknown) { throw new Error(`replay Trigger deliveries: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
      }
      await source.run(context, new UndeliverableConsumingTarget(target, logger));
    },
  };
}

/** The target a Trigger runner passes to its source; it logs and consumes undeliverable events. */
class UndeliverableConsumingTarget<Payload> implements TriggerTarget<Payload> {
  constructor(private readonly target: TriggerTarget<Payload>, private readonly logger: TriggerLogger) {}

  async handleTrigger(context: TriggerContext, event: TriggerEvent<Payload>): Promise<void> {
    try { await this.target.handleTrigger(context, event); }
    catch (error: unknown) {
      if (!isTriggerUndeliverable(error)) throw error;
      logTriggerRecord(this.logger, context, 'warn', 'trigger event skipped: undeliverable', { event_id: event.id, ...triggerErrorAttributes(error) });
      context.recordSkip();
    }
  }

  async prepareTrigger(context: TriggerContext, event: TriggerEvent<Payload>): Promise<void> { await prepareTriggerDelivery(context, this.target, event); }
}

/**
 * Decides whether an application accepts a provider event for Dex routing. Returning false consumes the
 * event without starting a Flow or invoking an RPC. It must be deterministic and side-effect free,
 * because durable delivery may evaluate it again after a restart.
 */
export type TriggerFilter<Event> = (event: TriggerEvent<Event>) => boolean;
/** Maps one provider event to the stable application Flow ID that owns it. */
export type FlowIdResolver<Event> = (event: TriggerEvent<Event>) => string;
/** Maps one provider event to a Flow's start input. */
export type FlowInputMapper<Event, Input> = (event: TriggerEvent<Event>) => Input;
/** Maps one provider event to an application's RPC input. */
export type RPCInputMapper<Event, Input> = (event: TriggerEvent<Event>) => Input;
/** A Dex RPC handler method declared on a Flow, as Client.invokeRPC accepts it. */
export type TriggerRPCMethod<Input, Output> = (context: Context, input: Input) => RPCResult<Output> | Promise<RPCResult<Output>> | void | Promise<void>;

/**
 * Create a filtered target that starts a Dex Flow.
 *
 * The event ID is the Flow-start request ID and Flow ID reuse is disallowed, so a duplicate start
 * resolves even after the original Flow completes. An empty Flow ID or event ID, or an input that cannot
 * be encoded, rejects with an UndeliverableTriggerError. Every other error is rethrown unchanged for a
 * retry, including a start the Dex Server rejects as invalid, so the event replays once the Flow is fixed.
 * It logs INFO "trigger event skipped: filtered" when the filter rejects an event and DEBUG "trigger event
 * delivered" when Dex starts the Flow. The caller that consumes or retries a rejection logs it.
 */
export function newDexFlowTriggerTarget<Event, Input>(
  client: Client, flow: Flow<Input>, filterEvent: TriggerFilter<Event>, resolveFlowId: FlowIdResolver<Event>, mapToFlowInput: FlowInputMapper<Event, Input>, options: TriggerOptions = {},
): TriggerTarget<Event> {
  if (!client || !flow || !filterEvent || !resolveFlowId || !mapToFlowInput) throw new TypeError('Dex client, Flow, Trigger filter, Flow ID resolver, and Flow input mapper are required');
  const logger = withTriggerLoggerAttributes(options.logger, { target: 'flow_start' });
  return {
    async handleTrigger(context, event) {
      if (!filterEvent(event)) { logTriggerRecord(logger, context, 'info', 'trigger event skipped: filtered', { event_id: event.id }); return; }
      const flowId = resolveFlowId(event);
      if (!flowId?.trim() || !event.id?.trim()) throw markTriggerUndeliverable(new Error('Flow trigger requires Flow ID and event ID'));
      const input = mapToFlowInput(event);
      try {
        await client.startFlow(flow, flowId, input, { idReusePolicy: IdReusePolicy.DISALLOW, requestId: event.id });
        logTriggerRecord(logger, context, 'debug', 'trigger event delivered', { event_id: event.id, flow_id: flowId, duplicate: false });
      } catch (error: unknown) {
        if (!(error instanceof FlowAlreadyStartedError)) throw classifyDexTriggerError(error);
        logTriggerRecord(logger, context, 'debug', 'trigger event delivered', { event_id: event.id, flow_id: flowId, duplicate: true });
      }
    },
  };
}

/** Optional behavior of newDexRPCTriggerTarget. */
export interface RPCTriggerOptions extends TriggerOptions {
  /**
   * The RPC's wire name, used instead of rpcMethod.name to derive the request ID. Set it to the name
   * override given to `@rpc({ name })` or `declareRPCMethod` when the RPC has one, and whenever a build may
   * minify or rename handler methods, so every redelivery of an event keeps one request ID. It only feeds
   * the request ID; the Dex Client still resolves the RPC from rpcMethod. Omitted uses the handler's
   * JavaScript method name. It must contain a non-blank character and no NUL character. Setting it to the
   * current method name keeps the request IDs unchanged.
   */
  rpcName?: string;
}

/**
 * The undeliverable failure of newDexRPCTriggerTarget when Dex Server reports that the event's request ID
 * "was already used for a different request": this attempt mapped the event to a different RPC request
 * than an earlier attempt did.
 *
 * Dex compares the whole request, so the cause is a non-deterministic or changed input mapper, another
 * binding that routes the same event to the same RPC on the same Flow, or changed RPC options such as the
 * timeout, locks, loads, or transactional flag. Trigger delivery consumes the event: it is dropped. Dex
 * reports the same conflict while the earlier request is still in flight, so when that request later
 * fails without committing, the event is lost. cause is the DexServiceError.
 */
export class TriggerRPCRequestConflictError extends UndeliverableTriggerError {
  /**
   * Describe the conflict of one delivery attempt.
   * @param eventId - The Trigger event ID.
   * @param flowId - The resolved Flow ID.
   * @param rpcName - The RPC name in the request ID: RPCTriggerOptions.rpcName or the handler's method name.
   * @param requestId - The request ID this attempt sent, from triggerRPCRequestId.
   * @param cause - The DexServiceError that reported the conflict.
   */
  constructor(readonly eventId: string, readonly flowId: string, readonly rpcName: string, readonly requestId: string, cause: unknown) {
    super(cause);
    this.message = `${UndeliverableTriggerMessage}: RPC trigger event ${eventId} for RPC ${rpcName} on Flow ${flowId} mapped to a different RPC request than an earlier attempt `
      + `with request ID ${requestId} (non-deterministic or changed input mapper, another binding, or changed RPC options); the event was dropped`;
    this.name = 'TriggerRPCRequestConflictError';
  }
}

/**
 * Create a filtered target that invokes one RPC on the resolved Flow.
 *
 * Every delivery attempt of an event passes the same request ID, triggerRPCRequestId(rpcName, event.id),
 * where rpcName is options.rpcName or else rpcMethod.name, so Dex Server applies the RPC's Flow changes at
 * most once per event while the Flow run keeps that request ID. A run keeps its newest request IDs only,
 * as many as the server limit limits.maxRequestIdsPerRun (1000 by default), shared with its other requests
 * that carry request IDs, such as Client Attribute writes; a redelivery after its ID was evicted applies
 * again. Declare the handler transactional (`isTransactional: true`) or with Attribute locks, so a repeat
 * returns the recorded reply without running the handler again. Any other handler runs again on a repeat,
 * and Dex discards its Flow changes when it makes any. A handler that must also ignore repeats beyond that
 * window keeps its own record of event IDs.
 *
 * The ID covers the RPC name and the event ID only, never the mapped input or the binding. Without
 * options.rpcName, renaming the method, or a build that minifies method names, changes the IDs of events
 * still being redelivered. Two bindings that route one event to the same RPC on the same Flow share one ID.
 *
 * Dex rejects a repeat whose request differs, such as one whose input mapper returned another value, with
 * INVALID_ARGUMENT "request ID ... was already used for a different request". A deterministic mapper never
 * causes it, because a repeat then sends an identical request, which Dex joins while it is in flight and
 * answers from the recorded reply once it committed. The target logs such a conflict at ERROR and rejects
 * with TriggerRPCRequestConflictError, an UndeliverableTriggerError, so delivery drops the event. Dex
 * reports the same conflict while the earlier request is still in flight; when that request later fails
 * without committing, the event is lost. Keep mapToRPCInput deterministic and set options.rpcName where
 * the method name can change.
 *
 * The Dex Client reads an input when the RPC handler declares two or more parameters or an input codec,
 * which the target cannot see, so the target picks its call as follows:
 * - mapToRPCInput undefined: an input-free call with the request ID; a handler that declares two or more
 *   parameters requires a mapper.
 * - a handler with two or more parameters, or a mapper that returns a value other than undefined: the
 *   mapped input with the request ID. An input-free RPC therefore needs no mapper, or one that returns
 *   undefined.
 * - a shorter handler whose mapper returns undefined: the RPC may be input-free or take an input codec, so
 *   the target invokes it as 0.10.0 did, without a request ID. Omit the mapper of an input-free RPC to
 *   deduplicate its deliveries.
 * - a handler whose name is empty or contains NUL, without options.rpcName: also without a request ID.
 *
 * It rejects with an UndeliverableTriggerError when the Flow has closed or was never started, when the
 * RPC handler throws an UndeliverableTriggerError, when the input cannot be encoded, when the Flow ID or
 * event ID is empty, or on a request ID conflict. It cannot tell an event that arrived before its Flow started from one whose Flow
 * never existed, so deliver a Flow's start before its RPC events. A response that cannot be decoded after
 * Dex applied the RPC resolves, because the target discards the output. Every other error is rethrown
 * unchanged for a retry. It logs INFO "trigger event skipped: filtered", DEBUG "trigger event delivered",
 * ERROR "trigger rpc request conflict; event dropped" with the event, request, RPC, and Flow IDs before
 * rejecting with a TriggerRPCRequestConflictError, and WARN when Dex applied the RPC but its response
 * cannot be decoded.
 *
 * It throws a TypeError at construction when a required argument is missing, when mapToRPCInput is
 * undefined for a handler that declares two or more parameters, or when options.rpcName is blank or
 * contains NUL.
 */
export function newDexRPCTriggerTarget<Event, Input, Output>(
  client: Client, rpcMethod: TriggerRPCMethod<Input, Output>, filterEvent: TriggerFilter<Event>, resolveFlowId: FlowIdResolver<Event>, mapToRPCInput: RPCInputMapper<Event, Input> | undefined,
  options: RPCTriggerOptions = {},
): TriggerTarget<Event> {
  if (!client || !rpcMethod || !filterEvent || !resolveFlowId) throw new TypeError('Dex client, RPC, Trigger filter, and Flow ID resolver are required');
  if (!mapToRPCInput && rpcMethod.length >= 2) throw new TypeError('RPC trigger requires an RPC input mapper for a handler declared with (context, input) parameters');
  if (options.rpcName !== undefined && !isTriggerRPCNameUsable(options.rpcName)) throw new TypeError('RPC trigger rpcName must contain a non-blank character and no NUL characters');
  const rpcName = options.rpcName ?? (isTriggerRPCNameUsable(rpcMethod.name) ? rpcMethod.name : undefined);
  const logger = withTriggerLoggerAttributes(options.logger, { target: 'rpc' });
  // The Client overloads cannot type a handler whose input presence is decided at run time.
  const rpcClient = client as unknown as {
    invokeRPC(method: TriggerRPCMethod<Input, Output>, flowId: string, input: Input | undefined): Promise<unknown>;
    invokeRPCWithOptions(method: TriggerRPCMethod<Input, Output>, flowId: string, ...inputAndOptions: unknown[]): Promise<unknown>;
  };
  return {
    async handleTrigger(context, event) {
      if (!filterEvent(event)) { logTriggerRecord(logger, context, 'info', 'trigger event skipped: filtered', { event_id: event.id }); return; }
      const flowId = resolveFlowId(event);
      if (!flowId?.trim() || !event.id?.trim()) throw markTriggerUndeliverable(new Error('RPC trigger requires Flow ID and event ID'));
      let requestId: string | undefined;
      try {
        if (!mapToRPCInput) {
          requestId = rpcName === undefined ? undefined : triggerRPCRequestId(rpcName, event.id);
          await (requestId === undefined ? rpcClient.invokeRPC(rpcMethod, flowId, undefined) : rpcClient.invokeRPCWithOptions(rpcMethod, flowId, { requestId } satisfies RPCInvokeOptions));
        } else {
          const input = mapToRPCInput(event);
          // Only these shapes certainly reach the Client's input-bearing overload.
          const isInputCertain = rpcMethod.length >= 2 || input !== undefined;
          requestId = rpcName === undefined || !isInputCertain ? undefined : triggerRPCRequestId(rpcName, event.id);
          await (requestId === undefined ? rpcClient.invokeRPC(rpcMethod, flowId, input) : rpcClient.invokeRPCWithOptions(rpcMethod, flowId, input, { requestId } satisfies RPCInvokeOptions));
        }
        logTriggerRecord(logger, context, 'debug', 'trigger event delivered', { event_id: event.id, flow_id: flowId, ...(requestId === undefined ? {} : { request_id: requestId }) });
      } catch (error: unknown) {
        const requestAttributes = requestId === undefined ? {} : { request_id: requestId };
        if (error instanceof ValueMappingError && error.operation === 'decode') {
          logTriggerRecord(logger, context, 'warn', 'trigger event delivered; rpc response undecodable', { event_id: event.id, flow_id: flowId, ...requestAttributes, ...triggerErrorAttributes(error) });
          return;
        }
        if (rpcName !== undefined && requestId !== undefined && isRequestIdUsedForDifferentRequest(error, requestId)) {
          logTriggerRecord(logger, context, 'error', 'trigger rpc request conflict; event dropped', { event_id: event.id, request_id: requestId, rpc_name: rpcName, flow_id: flowId });
          throw new TriggerRPCRequestConflictError(event.id, flowId, rpcName, requestId, error);
        }
        throw classifyDexTriggerError(error);
      }
    },
  };
}

/** Report whether a name can derive a request ID: it has a non-blank character and no NUL. */
function isTriggerRPCNameUsable(name: string): boolean {
  return name.trim() !== '' && !name.includes('\0');
}

/** Prefix of every request ID that newDexRPCTriggerTarget derives. */
export const TriggerRPCRequestIdPrefix = 'trigger-rpc-';

/**
 * Derive the Dex request ID that newDexRPCTriggerTarget sends for every delivery attempt of one event.
 *
 * The ID is TriggerRPCRequestIdPrefix followed by the first 16 bytes, as unpadded base64url (22
 * characters), of the SHA-256 digest of the UTF-8 bytes of rpcName, one 0x00 byte, and the UTF-8 bytes of
 * eventId; an unpaired UTF-16 surrogate is encoded as U+FFFD. rpcName is RPCTriggerOptions.rpcName or the
 * handler's JavaScript method name. For example, rpcName "receiveReply" and eventId "Ev1" hash the 16
 * bytes "receiveReply\x00Ev1" and yield "trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA". Throws a TypeError when
 * rpcName is empty or contains NUL.
 */
export function triggerRPCRequestId(rpcName: string, eventId: string): string {
  if (!rpcName || rpcName.includes('\0')) throw new TypeError('RPC request ID requires an RPC name without NUL characters');
  const digest = createHash('sha256').update(rpcName, 'utf8').update(Uint8Array.of(0)).update(eventId, 'utf8').digest();
  return `${TriggerRPCRequestIdPrefix}${digest.subarray(0, 16).toString('base64url')}`;
}

const grpcInvalidArgumentCode = 3;

/**
 * Report whether Dex rejected this call because a different request holds its request ID, committed or
 * still in flight. The detail must name this call's own request ID, so another INVALID_ARGUMENT keeps its
 * own classification.
 */
function isRequestIdUsedForDifferentRequest(error: unknown, requestId: string): boolean {
  return error instanceof DexServiceError && error.code === grpcInvalidArgumentCode
    && error.detail.includes(`request ID ${requestId} was already used for a different request`);
}

/**
 * Mark the Dex errors that no retry of the same event can fix: a closed or never-started Flow, an RPC
 * handler's own UndeliverableTriggerError, and an input that cannot be encoded. Every other error stays
 * retryable, because it depends on the Flow definition and server configuration rather than the event.
 */
export function classifyDexTriggerError(error: unknown): unknown {
  if (error === undefined || isTriggerUndeliverable(error)) return error;
  if (error instanceof FlowNotActiveOrNotFoundError || error instanceof FlowNotFoundError) return markTriggerUndeliverable(error);
  if (error instanceof WorkerInvocationError) return isHandlerUndeliverableMarker(error) ? markTriggerUndeliverable(error) : error;
  if (error instanceof ValueMappingError && error.operation === 'encode') return markTriggerUndeliverable(error);
  return error;
}

/**
 * Report whether a Worker failure is an RPC handler's own UndeliverableTriggerError: the Worker reports the
 * SDK-owned error type UndeliverableTriggerErrorType and a detail that is exactly the marker message or
 * starts with it and ": ". A Worker code alone, such as FAILED_PRECONDITION or UNKNOWN, never qualifies,
 * so another error with similar text keeps the event pending for a retry.
 */
function isHandlerUndeliverableMarker(error: WorkerInvocationError): boolean {
  const detail = error.workerErrorDetail;
  return error.workerErrorType === UndeliverableTriggerErrorType && (detail === UndeliverableTriggerMessage || detail.startsWith(`${UndeliverableTriggerMessage}: `));
}
