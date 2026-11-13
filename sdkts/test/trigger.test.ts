// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { FlowAlreadyStartedError, FlowNotActiveOrNotFoundError, ValueMappingError, WorkerInvocationError, type Client, type Flow } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import {
  UndeliverableTriggerError, UndeliverableTriggerErrorType, classifyDexTriggerError, deliverTrigger, isTriggerUndeliverable, markTriggerUndeliverable, mustTriggerBindingDefinition, newDexFlowTriggerTarget,
  newDexRPCTriggerTarget, newTrigger, newTriggerContext, triggerAttempt, triggerRetryDelayMs, withTriggerLogAttributes,
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
    const client = { invokeRPC: async (...args: unknown[]) => { invoked.push(args); const failure = failures.shift(); if (failure) throw failure; } } as unknown as Client;
    const method = () => undefined;
    const target = newDexRPCTriggerTarget(client, method, () => true, received => `flow-${received.id}`, received => ({ eventId: received.id }), { logger: newRecordingLogger() });
    await target.handleTrigger(newTriggerContext(), event);
    await expect(target.handleTrigger(newTriggerContext(), event)).rejects.toBe(otherFailure);
    expect(invoked[0]).toEqual([method, 'flow-Ev1', { eventId: 'Ev1' }]);
  });
});
