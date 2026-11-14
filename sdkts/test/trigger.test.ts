// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { DexServiceError, FlowAlreadyStartedError, FlowNotActiveOrNotFoundError, ValueMappingError, WorkerInvocationError, type Client, type Flow } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import {
  TriggerRPCRequestIdPrefix, UndeliverableTriggerError, UndeliverableTriggerErrorType, classifyDexTriggerError, deliverTrigger, isTriggerUndeliverable, markTriggerUndeliverable, mustTriggerBindingDefinition,
  TriggerRPCRequestConflictError, newDexFlowTriggerTarget, newDexRPCTriggerTarget, newTrigger, newTriggerContext, triggerAttempt, triggerRPCRequestId, triggerRetryDelayMs, withTriggerLogAttributes,
  type TriggerEvent, type TriggerTarget,
} from '../src/index.js';
import { newRecordingLogger } from './trigger_test_support.js';

const event: TriggerEvent<{ text: string }> = { id: 'Ev1', occurredAt: new Date('2025-10-12T00:00:00Z'), payload: { text: 'SENTINEL payload' } };
const definition = { trigger: { connectorId: 'sample', triggerName: 'itemCreated' }, description: 'Receive a new item.' };
const binding = { connection: { provider: 'sample', name: 'workspace' }, trigger: definition.trigger, name: 'start' };

describe('Trigger delivery', () => {
  it('retries a retryable failure on the capped exponential schedule and logs recovery once', async () => {
    expect([1, 2, 3, 8, 9].map(triggerRetryDelayMs)).toEqual([250, 500, 1000, 30_000, 30_000]);
    const logger = newRecordingLogger();
    let attempts = 0;
    class ProviderTimeoutError extends Error {}
    await deliverTrigger(withTriggerLogAttributes(newTriggerContext(), { channel: 'C1' }), { handleTrigger: async () => {
      if (++attempts < 2) throw new ProviderTimeoutError('token=SENTINEL-SECRET payload=SENTINEL payload', { cause: new TypeError('SENTINEL cause') });
    } }, event, { logger });
    expect(attempts).toBe(2);
    expect(logger.records.map(record => record.message)).toEqual(['trigger delivery failed; retrying', 'trigger delivered after retry']);
    expect(logger.records[0]!.attributes).toEqual({ event_id: 'Ev1', attempt: 1, delay_ms: 250, channel: 'C1', error_type: 'ProviderTimeoutError', error_cause_type: 'TypeError' });
    expect(JSON.stringify(logger.records)).not.toContain('SENTINEL');
  });

  it('consumes an undeliverable event and stops waiting once the context aborts', async () => {
    const logger = newRecordingLogger();
    await deliverTrigger(newTriggerContext(), { handleTrigger: async () => { throw markTriggerUndeliverable(new Error('SENTINEL secret reason')); } }, event, { logger });
    expect(logger.records).toEqual([{ level: 'warn', message: 'trigger event skipped: undeliverable', attributes: { event_id: 'Ev1', error_type: 'UndeliverableTriggerError', error_cause_type: 'Error' } }]);
    expect(JSON.stringify(logger.records)).not.toContain('SENTINEL');
    const controller = new AbortController();
    const pending = deliverTrigger(newTriggerContext(controller.signal), { handleTrigger: async () => { throw new Error('retry'); } }, event, { logger });
    setTimeout(() => controller.abort(), 20);
    await expect(pending).rejects.toThrow('Trigger delivery was aborted');
  });

  it('marks errors once and recognizes a wrapped marker', () => {
    const marked = markTriggerUndeliverable(new Error('reason'));
    expect(marked.message).toBe('Trigger event is undeliverable: reason');
    expect(markTriggerUndeliverable(marked)).toBe(marked);
    expect(isTriggerUndeliverable(new Error('outer', { cause: marked }))).toBe(true);
    expect(isTriggerUndeliverable(new Error('plain'))).toBe(false);
    expect(new UndeliverableTriggerError().message).toBe('Trigger event is undeliverable');
  });

  it('records a skip inside an attempt without reporting recovery', async () => {
    const logger = newRecordingLogger();
    let attempts = 0;
    await deliverTrigger(newTriggerContext(), { handleTrigger: async context => { if (++attempts === 1) throw new Error('retry'); context.recordSkip(); } }, event, { logger });
    expect(logger.records.map(record => record.message)).toEqual(['trigger delivery failed; retrying']);
    const started = triggerAttempt(newTriggerContext());
    started.context.recordSkip();
    expect(started.wasSkipped()).toBe(true);
  });
});

describe('Trigger runner', () => {
  it('replays first, forwards preparation, and consumes undeliverable events with the binding identity', async () => {
    const calls: string[] = [];
    const logger = newRecordingLogger();
    const target: TriggerTarget<{ text: string }> = {
      replayTriggerDeliveries: async () => { calls.push('replay'); },
      prepareTrigger: async (_context, prepared) => { calls.push(`prepare:${prepared.id}`); },
      handleTrigger: async () => { calls.push('handle'); throw markTriggerUndeliverable(new Error('no Flow')); },
    };
    const runner = newTrigger({ definition, binding, logger, target, source: { run: async (context, sourceTarget) => {
      await sourceTarget.prepareTrigger?.(context, event);
      const started = triggerAttempt(context);
      await sourceTarget.handleTrigger(started.context, event);
      calls.push(`skipped:${started.wasSkipped()}`);
    } } });
    await runner.run(newTriggerContext());
    expect(calls).toEqual(['replay', 'prepare:Ev1', 'handle', 'skipped:true']);
    expect(logger.records[0]).toMatchObject({ message: 'trigger event skipped: undeliverable', attributes: { connector: 'sample', connection: 'workspace', trigger: 'itemCreated', binding: 'start', event_id: 'Ev1' } });
    expect(runner.binding()).toEqual(binding);
  });

  it('rejects a binding for another Trigger and invalid static metadata', () => {
    const source = { run: async () => undefined };
    expect(() => newTrigger({ definition, binding: { ...binding, trigger: { connectorId: 'sample', triggerName: 'other' } }, source, target: { handleTrigger: async () => undefined } })).toThrow('does not match');
    expect(() => mustTriggerBindingDefinition({ definition, connectionName: ' ', bindingName: 'start' })).toThrow();
    expect(mustTriggerBindingDefinition({ definition, connectionName: 'workspace', bindingName: 'start' }).bindingName).toBe('start');
  });
});

describe('Dex Trigger targets', () => {
  const flow = { getFlowType: () => 'Sample', getSteps: () => { throw new Error('unused'); } } as unknown as Flow<{ text: string }>;

  it('starts a Flow with the event ID as request ID and treats an already started Flow as delivered', async () => {
    const starts: unknown[] = [];
    let isStarted = false;
    const client = { startFlow: async (...args: unknown[]) => {
      starts.push(args);
      if (isStarted) throw new FlowAlreadyStartedError(6, 'flowAlreadyStarted' as never, 'exists', 'startFlow', 'flow-Ev1');
      isStarted = true;
      return 'run';
    } } as unknown as Client;
    const logger = newRecordingLogger();
    const target = newDexFlowTriggerTarget<{ text: string }, { text: string }>(client, flow, () => true, received => `flow-${received.id}`, received => received.payload, { logger });
    await target.handleTrigger(newTriggerContext(), event);
    await target.handleTrigger(newTriggerContext(), event);
    expect(starts[0]).toEqual([flow, 'flow-Ev1', event.payload, { idReusePolicy: 'disallow', requestId: 'Ev1' }]);
    expect(logger.records.map(record => record.attributes.duplicate)).toEqual([false, true]);
    await expect(newDexFlowTriggerTarget<{ text: string }, { text: string }>(client, flow, () => true, () => ' ', received => received.payload).handleTrigger(newTriggerContext(), event)).rejects.toBeInstanceOf(UndeliverableTriggerError);
    const filtered = newRecordingLogger();
    await newDexFlowTriggerTarget<{ text: string }, { text: string }>(client, flow, () => false, () => 'unused', received => received.payload, { logger: filtered }).handleTrigger(newTriggerContext(), event);
    expect(filtered.records[0]!.message).toBe('trigger event skipped: filtered');
  });

  it('classifies RPC failures as Go does', async () => {
    const workerFailure = (code: number, type: string, detail: string) => new WorkerInvocationError(9, 'workerApiError' as never, 'x', 'invokeRPC', 'f', code, type, detail);
    const thrown = new UndeliverableTriggerError(new Error('closed'));
    expect(thrown.name).toBe(UndeliverableTriggerErrorType);
    const handlerMarker = workerFailure(2, thrown.name, thrown.message);
    const bareMarker = workerFailure(2, UndeliverableTriggerErrorType, 'Trigger event is undeliverable');
    const otherFailure = workerFailure(2, 'Error', 'thread is still loading');
    expect(isTriggerUndeliverable(classifyDexTriggerError(handlerMarker))).toBe(true);
    expect(isTriggerUndeliverable(classifyDexTriggerError(bareMarker))).toBe(true);
    expect(isTriggerUndeliverable(classifyDexTriggerError(otherFailure))).toBe(false);
    // Neither a generic Worker code nor a lookalike error name with the marker's text consumes the event.
    for (const lookalike of [
      workerFailure(9, 'error', 'Trigger event is undeliverable: closed'),
      workerFailure(2, 'UndeliverableTriggerError', 'Trigger event is undeliverable: closed'),
      workerFailure(2, 'FlowNotActiveOrNotFoundError', 'Trigger event is undeliverable: closed'),
      workerFailure(2, UndeliverableTriggerErrorType, 'Trigger event is undeliverable because a provider said so'),
      workerFailure(2, UndeliverableTriggerErrorType, 'error: Trigger event is undeliverable'),
    ]) expect(isTriggerUndeliverable(classifyDexTriggerError(lookalike)), lookalike.workerErrorDetail).toBe(false);
    expect(isTriggerUndeliverable(classifyDexTriggerError(new FlowNotActiveOrNotFoundError(5, 'flowNotFound' as never, 'gone', 'invokeRPC', 'f')))).toBe(true);
    expect(isTriggerUndeliverable(classifyDexTriggerError(new ValueMappingError('encode', 'bad')))).toBe(true);
    expect(isTriggerUndeliverable(classifyDexTriggerError(new Error('unavailable')))).toBe(false);

    const failures: unknown[] = [new ValueMappingError('decode', 'bad output'), otherFailure];
    const invoked: unknown[] = [];
    const client = { invokeRPCWithOptions: async (...args: unknown[]) => { invoked.push(args); const failure = failures.shift(); if (failure) throw failure; } } as unknown as Client;
    const method = (_context: unknown, input: unknown) => ({ output: input });
    const target = newDexRPCTriggerTarget(client, method, () => true, received => `flow-${received.id}`, received => ({ eventId: received.id }), { logger: newRecordingLogger() });
    await target.handleTrigger(newTriggerContext(), event);
    await expect(target.handleTrigger(newTriggerContext(), event)).rejects.toBe(otherFailure);
    expect(invoked[0]).toEqual([method, 'flow-Ev1', { eventId: 'Ev1' }, { requestId: triggerRPCRequestId('method', 'Ev1') }]);
  });

  it('sends one request ID per RPC and event whatever input the mapper returns', async () => {
    const calls: { input: unknown; requestId: string }[] = [];
    let attempts = 0;
    const client = { invokeRPCWithOptions: async (_method: unknown, _flowId: string, input: unknown, options: { requestId: string }) => {
      calls.push({ input, requestId: options.requestId });
      if (++attempts === 1) throw new Error('UNAVAILABLE');
    } } as unknown as Client;
    const recordApproval = (_context: unknown, input: unknown) => ({ output: input });
    const recordAudit = (_context: unknown, input: unknown) => ({ output: input });
    const logger = newRecordingLogger();
    let mapperCalls = 0;
    // A mapper that returns another value on every call, as one reading the clock would.
    const mapToChangingInput = (received: TriggerEvent<{ text: string }>) => ({ eventId: received.id, mappedAt: ++mapperCalls });
    const approvalTarget = newDexRPCTriggerTarget(client, recordApproval, () => true, () => 'flow-1', mapToChangingInput, { logger });
    await deliverTrigger(newTriggerContext(), approvalTarget, event, { logger });
    await approvalTarget.handleTrigger(newTriggerContext(), event);
    await newDexRPCTriggerTarget(client, recordAudit, () => true, () => 'flow-1', mapToChangingInput, { logger }).handleTrigger(newTriggerContext(), event);
    await approvalTarget.handleTrigger(newTriggerContext(), { ...event, id: 'Ev2' });
    const [first, retried, redelivered, otherRPC, otherEvent] = calls.map(call => call.requestId);
    expect(attempts).toBe(5);
    expect(new Set(calls.slice(0, 3).map(call => JSON.stringify(call.input))).size).toBe(3);
    expect([retried, redelivered]).toEqual([first, first]);
    expect(new Set([first, otherRPC, otherEvent]).size).toBe(3);
    expect(first).toBe(triggerRPCRequestId('recordApproval', 'Ev1'));
    expect(logger.records.find(record => record.message === 'trigger event delivered')?.attributes.request_id).toBe(first);
  });

  it('derives the documented request ID from the RPC name and event ID bytes', () => {
    // SHA-256 of the bytes 72 65 63 65 69 76 65 52 65 70 6c 79 00 45 76 31 ("receiveReply", NUL, "Ev1"), first 16 bytes as base64url.
    expect(triggerRPCRequestId('receiveReply', 'Ev1')).toBe('trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA');
    expect(triggerRPCRequestId('receiveReply', 'Ev1')).toMatch(new RegExp(`^${TriggerRPCRequestIdPrefix}[A-Za-z0-9_-]{22}$`));
    // The NUL separator keeps the split between method name and event ID unambiguous.
    expect(triggerRPCRequestId('receiveReplyE', 'v1')).not.toBe(triggerRPCRequestId('receiveReply', 'Ev1'));
    expect(triggerRPCRequestId('receiveReply', 'Ev1\0x')).not.toBe(triggerRPCRequestId('receiveReply', 'Ev1'));
    expect(() => triggerRPCRequestId('', 'Ev1')).toThrow(TypeError);
    expect(() => triggerRPCRequestId('receive\0Reply', 'Ev1')).toThrow(TypeError);
  });

  it('drops the event on its first request ID conflict and logs the drop at ERROR without the input', async () => {
    const receiveReply = (_context: unknown, input: unknown) => ({ output: input });
    const requestId = triggerRPCRequestId('receiveReply', 'Ev1');
    const serviceFailure = (code: number, detail: string) => new DexServiceError(code as never, 'uncategorized' as never, detail, 'invokeRPC', 'flow-1');
    const conflict = serviceFailure(3, `request ID ${requestId} was already used for a different request`);
    let attempts = 0;
    const client = { invokeRPCWithOptions: async () => { attempts++; throw conflict; } } as unknown as Client;
    const logger = newRecordingLogger();
    const target = newDexRPCTriggerTarget(client, receiveReply, () => true, () => 'flow-1', (received: TriggerEvent<{ text: string }>) => ({ eventId: received.id, text: received.payload.text }), { logger });
    const rejection = await target.handleTrigger(newTriggerContext(), event).then(() => undefined, (error: unknown) => error);
    expect(rejection).toBeInstanceOf(TriggerRPCRequestConflictError);
    expect(rejection).toBeInstanceOf(UndeliverableTriggerError);
    expect(isTriggerUndeliverable(rejection)).toBe(true);
    const conflictError = rejection as TriggerRPCRequestConflictError;
    expect(conflictError.cause).toBe(conflict);
    expect([conflictError.eventId, conflictError.flowId, conflictError.rpcName, conflictError.requestId]).toEqual(['Ev1', 'flow-1', 'receiveReply', requestId]);
    expect(conflictError.message).toContain('mapped to a different RPC request than an earlier attempt');
    expect(conflictError.message).toContain('the event was dropped');
    expect(logger.records).toEqual([{ level: 'error', message: 'trigger rpc request conflict; event dropped', attributes: { target: 'rpc', event_id: 'Ev1', request_id: requestId, rpc_name: 'receiveReply', flow_id: 'flow-1' } }]);
    expect(JSON.stringify(logger.records)).not.toContain('SENTINEL');

    // deliverTrigger consumes it on the first attempt instead of retrying.
    const deliveryLogger = newRecordingLogger();
    await deliverTrigger(newTriggerContext(), target, event, { logger: deliveryLogger });
    expect(attempts).toBe(2);
    expect(deliveryLogger.records.map(record => [record.level, record.message])).toEqual([['warn', 'trigger event skipped: undeliverable']]);
    expect(deliveryLogger.records[0]!.attributes).toMatchObject({ error_type: 'TriggerRPCRequestConflictError', error_cause_type: 'DexServiceError' });

    // options.rpcName is the RPC name the drop reports.
    const wiredLogger = newRecordingLogger();
    const wiredRequestId = triggerRPCRequestId('receiveThreadReply', 'Ev1');
    const wiredClient = { invokeRPCWithOptions: async () => { throw serviceFailure(3, `request ID ${wiredRequestId} was already used for a different request`); } } as unknown as Client;
    const wiredTarget = newDexRPCTriggerTarget(wiredClient, receiveReply, () => true, () => 'flow-1', received => ({ eventId: received.id }), { logger: wiredLogger, rpcName: 'receiveThreadReply' });
    await expect(wiredTarget.handleTrigger(newTriggerContext(), event)).rejects.toMatchObject({ rpcName: 'receiveThreadReply', requestId: wiredRequestId });
    expect(wiredLogger.records[0]).toMatchObject({ level: 'error', attributes: { rpc_name: 'receiveThreadReply', request_id: wiredRequestId } });

    // Only this request ID's conflict counts; every other INVALID_ARGUMENT and code keeps its own classification.
    for (const failure of [
      serviceFailure(3, 'request ID trigger-rpc-other was already used for a different request'),
      serviceFailure(9, `request ID ${requestId} was already used for a different request`),
      serviceFailure(3, 'RPC input is invalid'),
    ]) {
      const failingClient = { invokeRPCWithOptions: async () => { throw failure; } } as unknown as Client;
      const failingTarget = newDexRPCTriggerTarget(failingClient, receiveReply, () => true, () => 'flow-1', received => ({ eventId: received.id }), { logger: newRecordingLogger() });
      await expect(failingTarget.handleTrigger(newTriggerContext(), event), failure.detail).rejects.toBe(failure);
    }
    // The exported classifier cannot see the request ID, so it keeps every INVALID_ARGUMENT retryable.
    expect(isTriggerUndeliverable(classifyDexTriggerError(conflict))).toBe(false);
  });

  it('picks the Dex Client call per RPC shape and passes the request ID whenever the shape is certain', async () => {
    const calls: { overload: string; args: unknown[] }[] = [];
    const client = {
      invokeRPCWithOptions: async (...args: unknown[]) => { calls.push({ overload: 'invokeRPCWithOptions', args }); },
      invokeRPC: async (...args: unknown[]) => { calls.push({ overload: 'invokeRPC', args }); },
    } as unknown as Client;
    const deliver = async (rpcMethod: (...args: never[]) => unknown, mapToRPCInput: ((received: TriggerEvent<{ text: string }>) => unknown) | undefined) => {
      calls.length = 0;
      await newDexRPCTriggerTarget(client, rpcMethod as never, () => true, () => 'flow-1', mapToRPCInput, { logger: newRecordingLogger() }).handleTrigger(newTriggerContext(), event);
      return calls[0];
    };
    const receiveReply = (_context: unknown, input: unknown) => ({ output: input });
    // A default-valued input parameter leaves length 1; the Client reads its input through the RPC's inputCodec.
    function recordDefaultedAmount(_context: unknown, input: unknown = { amount: 1 }) { return { output: input }; }
    const describeApproval = (context: unknown) => ({ output: context });
    expect(await deliver(receiveReply, received => ({ eventId: received.id }))).toEqual({ overload: 'invokeRPCWithOptions', args: [receiveReply, 'flow-1', { eventId: 'Ev1' }, { requestId: triggerRPCRequestId('receiveReply', 'Ev1') }] });
    expect(await deliver(recordDefaultedAmount, () => ({ amount: 2 }))).toEqual({ overload: 'invokeRPCWithOptions', args: [recordDefaultedAmount, 'flow-1', { amount: 2 }, { requestId: triggerRPCRequestId('recordDefaultedAmount', 'Ev1') }] });
    expect(await deliver(describeApproval, undefined)).toEqual({ overload: 'invokeRPCWithOptions', args: [describeApproval, 'flow-1', { requestId: triggerRPCRequestId('describeApproval', 'Ev1') }] });
    // A shorter handler whose mapper returns undefined may be input-free or take an input codec, so it keeps the 0.10.0 call.
    expect(await deliver(describeApproval, () => undefined)).toEqual({ overload: 'invokeRPC', args: [describeApproval, 'flow-1', undefined] });
    expect(await deliver(recordDefaultedAmount, () => undefined)).toEqual({ overload: 'invokeRPC', args: [recordDefaultedAmount, 'flow-1', undefined] });
    // A two-parameter handler certainly takes its input, even an undefined one.
    expect(await deliver(receiveReply, () => undefined)).toEqual({ overload: 'invokeRPCWithOptions', args: [receiveReply, 'flow-1', undefined, { requestId: triggerRPCRequestId('receiveReply', 'Ev1') }] });
    // Without options.rpcName, a handler name that cannot derive an ID also keeps the 0.10.0 call.
    const unnamed = Object.defineProperty((_context: unknown, input: unknown) => ({ output: input }), 'name', { value: '' });
    expect(await deliver(unnamed, received => ({ eventId: received.id }))).toEqual({ overload: 'invokeRPC', args: [unnamed, 'flow-1', { eventId: 'Ev1' }] });
  });

  it('derives the request ID from options.rpcName instead of the method name', async () => {
    const requestIds: string[] = [];
    const client = { invokeRPCWithOptions: async (_method: unknown, _flowId: string, _input: unknown, options: { requestId: string }) => { requestIds.push(options.requestId); } } as unknown as Client;
    const named = (name: string) => Object.defineProperty((_context: unknown, input: unknown) => ({ output: input }), 'name', { value: name });
    const deliver = async (rpcMethod: ReturnType<typeof named>, rpcName?: string) => {
      await newDexRPCTriggerTarget(client, rpcMethod, () => true, () => 'flow-1', received => ({ eventId: received.id }), { logger: newRecordingLogger(), ...(rpcName === undefined ? {} : { rpcName }) })
        .handleTrigger(newTriggerContext(), event);
      return requestIds.at(-1);
    };
    const receiveReplyId = triggerRPCRequestId('receiveReply', 'Ev1');
    expect(receiveReplyId).toBe('trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA');
    // A minified or renamed method keeps the request ID of its wire name.
    expect(await deliver(named('a'), 'receiveReply')).toBe(receiveReplyId);
    expect(await deliver(named('onReply'), 'receiveReply')).toBe(receiveReplyId);
    expect(await deliver(named(''), 'receiveReply')).toBe(receiveReplyId);
    expect(await deliver(named('receiveReply'))).toBe(receiveReplyId);
    expect(await deliver(named('a'))).toBe(triggerRPCRequestId('a', 'Ev1'));
    for (const rpcName of ['', '   ', 'receive\0Reply']) {
      expect(() => newDexRPCTriggerTarget(client, named('receiveReply'), () => true, () => 'flow-1', () => ({}), { rpcName }), JSON.stringify(rpcName)).toThrow('rpcName');
    }
  });

  it('accepts every handler that 0.10.0 accepted and rejects only missing arguments', () => {
    const client = { invokeRPCWithOptions: async () => undefined, invokeRPC: async () => undefined } as unknown as Client;
    const named = (name: string) => Object.defineProperty((_context: unknown, input: unknown) => ({ output: input }), 'name', { value: name });
    for (const rpcMethod of [() => undefined, (context: unknown) => ({ output: context }), named(''), named('receive\0Reply'), named('receiveReply')]) {
      expect(() => newDexRPCTriggerTarget(client, rpcMethod, () => true, () => 'flow-1', () => ({}))).not.toThrow();
    }
    expect(() => newDexRPCTriggerTarget(client, (context: unknown) => ({ output: context }), () => true, () => 'flow-1', undefined)).not.toThrow();
    expect(() => newDexRPCTriggerTarget(client, named('receiveReply'), () => true, () => 'flow-1', undefined)).toThrow('RPC input mapper');
    expect(() => newDexRPCTriggerTarget(undefined as unknown as Client, named('receiveReply'), () => true, () => 'flow-1', () => ({}))).toThrow(TypeError);
    expect(() => newDexRPCTriggerTarget(client, undefined as never, () => true, () => 'flow-1', () => ({}))).toThrow(TypeError);
  });
});
