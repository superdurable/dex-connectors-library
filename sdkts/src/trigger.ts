// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { FlowAlreadyStartedError, FlowNotActiveOrNotFoundError, FlowNotFoundError, IdReusePolicy, ValueMappingError, WorkerInvocationError, type Client, type Context, type Flow, type RPCResult } from '@superdurable/dex';
import { z } from 'zod';
import { ConnectionRefSchema, ConnectorConfigurationUISchema, type ConnectorConfigurationUI } from './types.js';
import {
  UndeliverableTriggerErrorType, UndeliverableTriggerMessage, isTriggerUndeliverable, logTriggerRecord, markTriggerUndeliverable, prepareTriggerDelivery, triggerErrorAttributes,
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

/**
 * Create a filtered target that invokes one RPC on the resolved Flow.
 *
 * It rejects with an UndeliverableTriggerError when the Flow has closed or was never started, when the
 * RPC handler throws an UndeliverableTriggerError, when the input cannot be encoded, or when the Flow ID
 * or event ID is empty. It cannot tell an event that arrived before its Flow started from one whose Flow
 * never existed, so deliver a Flow's start before its RPC events. A response that cannot be decoded
 * after Dex applied the RPC resolves, because the target discards the output. Every other error is
 * rethrown unchanged for a retry. Dex does not deduplicate retried RPCs, so the RPC must treat a repeated
 * event ID as a duplicate. It logs INFO "trigger event skipped: filtered", DEBUG "trigger event
 * delivered", and WARN when Dex applied the RPC but its response cannot be decoded.
 */
export function newDexRPCTriggerTarget<Event, Input, Output>(
  client: Client, rpcMethod: TriggerRPCMethod<Input, Output>, filterEvent: TriggerFilter<Event>, resolveFlowId: FlowIdResolver<Event>, mapToRPCInput: RPCInputMapper<Event, Input>, options: TriggerOptions = {},
): TriggerTarget<Event> {
  if (!client || !rpcMethod || !filterEvent || !resolveFlowId || !mapToRPCInput) throw new TypeError('Dex client, RPC, Trigger filter, Flow ID resolver, and RPC input mapper are required');
  const logger = withTriggerLoggerAttributes(options.logger, { target: 'rpc' });
  const invoke = client.invokeRPC.bind(client) as (method: TriggerRPCMethod<Input, Output>, flowId: string, input: Input) => Promise<unknown>;
  return {
    async handleTrigger(context, event) {
      if (!filterEvent(event)) { logTriggerRecord(logger, context, 'info', 'trigger event skipped: filtered', { event_id: event.id }); return; }
      const flowId = resolveFlowId(event);
      if (!flowId?.trim() || !event.id?.trim()) throw markTriggerUndeliverable(new Error('RPC trigger requires Flow ID and event ID'));
      try {
        await invoke(rpcMethod, flowId, mapToRPCInput(event));
        logTriggerRecord(logger, context, 'debug', 'trigger event delivered', { event_id: event.id, flow_id: flowId });
      } catch (error: unknown) {
        if (error instanceof ValueMappingError && error.operation === 'decode') {
          logTriggerRecord(logger, context, 'warn', 'trigger event delivered; rpc response undecodable', { event_id: event.id, flow_id: flowId, ...triggerErrorAttributes(error) });
          return;
        }
        throw classifyDexTriggerError(error);
      }
    },
  };
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
