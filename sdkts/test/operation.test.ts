// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError } from '../src/errors.js';
import { createCall, StaticCredentialProvider, resolveCredential, resolveCredentialAfterRejection, type Call, type CredentialRefreshDriver } from '../src/credentials.js';
import { validateMutationDefinition, validateQueryDefinition, type MutationDefinition, type QueryDefinition } from '../src/operation.js';
import { runMutation, runQuery } from '../src/run_operation.js';
import { deriveCallID, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt } from '../src/types.js';
import type { AsyncContext } from '@superdurable/dex';

const context = { attempt: 1, flowId: 'flow-1', stepExecutionId: 'step-1' } as AsyncContext;
const call = createCall(context, { provider: 'sample', name: 'default' }, { connectorId: 'sample', operationId: 'createItem' });
const failure = { kind: 'AVAILABILITY', provider: 'sample', operation: 'createItem', message: 'temporarily unavailable' } as const;
const mutationDefinition: MutationDefinition = { operation: call.operation, branches: [{ id: 'created', description: 'Created' }, { id: 'defect', description: 'Invalid connector', optional: true }, { id: 'uncertain', description: 'Write may have reached provider', optional: true }], stepDefaults: {} };
const queryDefinition: QueryDefinition = { ...mutationDefinition, branches: mutationDefinition.branches.filter(branch => branch.id !== 'uncertain') };

describe('operation execution', () => {
  it('completes a safe receipt and uses the stable mutation key', async () => {
    const result = await runMutation({ definition: () => mutationDefinition, idempotencyKey: id => `sample/${id}`, invoke: (receivedCall, input: string) => {
      expect(receivedCall.idempotencyKey).toBe(`sample/${call.id}`);
      expect(receivedCall.context).toBe(context);
      expect(JSON.stringify(receivedCall)).not.toContain('stepExecutionId');
      return newMutationBranch('created', input, { providerObjectId: 'item-1' });
    } }, call, 'item-1');
    expect(result).toMatchObject({ branch: 'created', value: 'item-1', receipt: { callId: call.id, idempotencyKey: `sample/${call.id}`, provider: 'sample', providerObjectId: 'item-1' } });
    expect(new Date(result.receipt.observedAt).getTime()).toBeGreaterThan(0);
  });

  it('encodes the Go zero-value receipt and completes it at the call boundary', async () => {
    expect(JSON.stringify(zeroReceipt())).toBe('{"callId":"","provider":"","observedAt":"0001-01-01T00:00:00Z"}');
    const result = await runQuery({ definition: () => queryDefinition, invoke: () => newQueryBranch('created', '', zeroReceipt()) }, call, undefined);
    expect(result.receipt.callId).toBe(call.id);
    expect(result.receipt.provider).toBe('sample');
    expect(result.receipt.observedAt).not.toBe('0001-01-01T00:00:00Z');
  });

  it('routes a declared query branch and rejects an undeclared one as defect', async () => {
    const query = { definition: () => queryDefinition, invoke: () => newQueryBranch('created', 'item-1') };
    expect((await runQuery(query, call, undefined)).branch).toBe('created');
    expect((await runQuery({ ...query, invoke: () => newQueryBranch('missing', '') }, call, undefined)).failure?.kind).toBe('LOCAL_DEFECT');
  });

  it('throws only a validated safe retry with provider delay', async () => {
    await expect(runQuery({ definition: () => queryDefinition, invoke: () => newQueryRetry(failure, 1500) }, call, undefined)).rejects.toMatchObject({ name: 'RetryError', retryAfterMs: 1500 });
    await expect(runMutation({ definition: () => mutationDefinition, idempotencyKey: () => '', invoke: () => newMutationRetry(failure) }, call, undefined)).rejects.toBeInstanceOf(RetryError);
    const bad = { kind: 'retry', failure: { ...failure, kind: 'UNKNOWN' }, retryAfterMs: 0 };
    const result = await runQuery({ definition: () => queryDefinition, invoke: () => bad as never }, call, undefined);
    expect(result).toMatchObject({ branch: 'defect', failure: { kind: 'LOCAL_DEFECT' } });
  });

  it('refuses a concurrent call of the same mutation before invoking it', async () => {
    const stepContext = { attempt: 1, flowId: 'flow-concurrent', stepExecutionId: 'step-1' } as AsyncContext;
    const stepCall = createCall(stepContext, { provider: 'sample', name: 'default' }, call.operation);
    let release!: () => void;
    const released = new Promise<void>(resolve => { release = resolve; });
    let started!: () => void;
    const hasStarted = new Promise<void>(resolve => { started = resolve; });
    let invocations = 0;
    const blocking = { definition: () => mutationDefinition, idempotencyKey: () => '', invoke: async (_call: unknown, input: string) => {
      invocations++;
      started();
      await released;
      return newMutationBranch('created', input);
    } };
    const first = runMutation(blocking, stepCall, 'first');
    await hasStarted;
    let isConcurrentInvoked = false;
    const concurrent = { definition: () => mutationDefinition, idempotencyKey: () => '', invoke: (_call: unknown, input: string) => { isConcurrentInvoked = true; return newMutationBranch('created', input); } };
    const refused = await runMutation(concurrent, stepCall, 'second');
    expect(refused).toMatchObject({ branch: 'defect', failure: { kind: 'LOCAL_DEFECT' } });
    expect(refused.failure?.message).toContain('concurrent calls');
    expect(isConcurrentInvoked).toBe(false);

    const otherOperation = { connectorId: 'sample', operationId: 'deleteItem' };
    const otherCall = createCall(stepContext, { provider: 'sample', name: 'default' }, otherOperation);
    const other = { ...concurrent, definition: () => ({ ...mutationDefinition, operation: otherOperation }) };
    expect((await runMutation(other, otherCall, 'other')).branch).toBe('created');

    release();
    expect((await first).branch).toBe('created');
    expect((await runMutation(concurrent, stepCall, 'third')).branch).toBe('created');
    expect(invocations).toBe(1);
  });

  it('gives the next begun mutation occurrence 2 after refused concurrent calls, as Go does', async () => {
    const stepContext = { attempt: 1, flowId: 'flow-ordinal', stepExecutionId: 'step-1', recordHeartbeat: async () => {} } as unknown as AsyncContext;
    const progress: { callId: string; sequence: number }[] = [];
    const progressStream = { write: (_context: unknown, update: { callId: string; sequence: number }) => { progress.push(update); } } as never;
    const derivedCall = () => createCall(stepContext, { provider: 'sample', name: 'default' }, call.operation, { progressStream });
    let release!: () => void;
    const released = new Promise<void>(resolve => { release = resolve; });
    let started!: () => void;
    const hasStarted = new Promise<void>(resolve => { started = resolve; });
    const blocking = { definition: () => mutationDefinition, idempotencyKey: () => '', invoke: async (_call: unknown, input: string) => {
      started();
      await released;
      return newMutationBranch('created', input);
    } };
    // Promise.all builds every Call before any mutation begins.
    const calls = [derivedCall(), derivedCall(), derivedCall()];
    const first = runMutation(blocking, calls[0]!, 'first');
    await hasStarted;
    const refused = await Promise.all(calls.slice(1).map(refusedCall => runMutation({ ...blocking, invoke: () => newMutationBranch('created', 'never') }, refusedCall, 'concurrent')));
    expect(refused.map(result => result.branch)).toEqual(['defect', 'defect']);
    release();
    expect((await first).receipt.callId).toBe(calls[0]!.id);

    const secondOccurrence = deriveCallID('flow-ordinal', 'step-1', call.operation, 2);
    const laterCall = derivedCall();
    expect(laterCall.id).not.toBe(secondOccurrence);
    let observedId = '';
    let observedKey = '';
    const later = await runMutation({ definition: () => mutationDefinition, idempotencyKey: () => '', invoke: (receivedCall: Call, input: string) => {
      observedId = receivedCall.id;
      observedKey = receivedCall.idempotencyKey ?? '';
      receivedCall.reportProgress({ phase: 'sending' });
      return newMutationBranch('created', input);
    } }, laterCall, 'later');
    expect([observedId, observedKey, later.receipt.callId]).toEqual([secondOccurrence, secondOccurrence, secondOccurrence]);
    expect(progress).toMatchObject([{ callId: secondOccurrence, sequence: 1 }]);
    let rerunId = '';
    await runMutation({ definition: () => mutationDefinition, idempotencyKey: () => '', invoke: (receivedCall: Call, input: string) => { rerunId = receivedCall.id; return newMutationBranch('created', input); } }, laterCall, 'rerun');
    expect(rerunId).toBe(secondOccurrence);

    const applicationCall = { ...laterCall, id: 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa' } as Call;
    Object.defineProperty(applicationCall, 'context', { value: stepContext, enumerable: false });
    let keptId = '';
    await runMutation({ definition: () => mutationDefinition, idempotencyKey: () => '', invoke: (receivedCall: Call, input: string) => { keptId = receivedCall.id; return newMutationBranch('created', input); } }, applicationCall, 'application');
    expect(keptId).toBe(applicationCall.id);
  });

  it('shifts occurrences only past calls refused before they began, never reusing a call ID', async () => {
    const stepContext = { attempt: 1, flowId: 'flow-shift', stepExecutionId: 'step-1' } as AsyncContext;
    const derivedCall = () => createCall(stepContext, { provider: 'sample', name: 'default' }, call.operation);
    const occurrence = (n: number) => deriveCallID('flow-shift', 'step-1', call.operation, n);
    const runAndObserve = async (target: Call): Promise<string | undefined> => {
      let observed: string | undefined;
      await runMutation({ definition: () => mutationDefinition, idempotencyKey: () => '', invoke: (receivedCall: Call, input: string) => { observed = receivedCall.id; return newMutationBranch('created', input); } }, target, 'input');
      return observed;
    };
    const runBlocking = async (target: Call) => {
      let release!: () => void;
      const released = new Promise<void>(resolve => { release = resolve; });
      let started!: () => void;
      const hasStarted = new Promise<void>(resolve => { started = resolve; });
      const result = runMutation({ definition: () => mutationDefinition, idempotencyKey: () => '', invoke: async (_call: unknown, input: string) => { started(); await released; return newMutationBranch('created', input); } }, target, 'blocking');
      await hasStarted;
      return { release, result };
    };

    const first = derivedCall();
    const running = await runBlocking(first);
    expect(await runAndObserve(first)).toBeUndefined();
    running.release();
    await running.result;
    expect(await runAndObserve(first)).toBe(occurrence(1));
    expect(derivedCall().id).toBe(occurrence(2));
    expect(await runAndObserve(derivedCall())).toBe(occurrence(3));

    const holder = await runBlocking(derivedCall());
    const refusedCall = derivedCall();
    expect(await runAndObserve(refusedCall)).toBeUndefined();
    holder.release();
    await holder.result;
    expect(await runAndObserve(derivedCall())).toBe(occurrence(5));
    let isCollisionInvoked = false;
    const collision = await runMutation({ definition: () => mutationDefinition, idempotencyKey: () => '', invoke: (_call: unknown, input: string) => { isCollisionInvoked = true; return newMutationBranch('created', input); } }, refusedCall, 'rerun');
    expect(collision).toMatchObject({ branch: 'defect', failure: { kind: 'LOCAL_DEFECT' } });
    expect(collision.failure?.message).toContain('reuse the call ID');
    expect(isCollisionInvoked).toBe(false);
  });

  it('allows uncertainty only for a declared mutation branch', async () => {
    const mutation = { definition: () => mutationDefinition, idempotencyKey: () => '', invoke: () => newMutationUncertain('', failure) };
    expect((await runMutation(mutation, call, undefined)).branch).toBe('uncertain');
    const withoutUncertainty = { ...mutationDefinition, branches: mutationDefinition.branches.filter(branch => branch.id !== 'uncertain') };
    expect((await runMutation({ ...mutation, definition: () => withoutUncertainty }, call, undefined)).failure?.kind).toBe('LOCAL_DEFECT');
  });

  it('rejects a receipt for another call and invalid definition branches', async () => {
    const query = { definition: () => queryDefinition, invoke: () => newQueryBranch('created', '', { callId: 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa' }) };
    expect((await runQuery(query, call, undefined)).failure?.message).toContain('another call');
    expect(() => validateQueryDefinition({ ...queryDefinition, branches: [...queryDefinition.branches, { id: 'uncertain', description: 'Invalid' }] })).toThrow('query cannot declare');
    expect(() => validateMutationDefinition({ ...mutationDefinition, branches: [mutationDefinition.branches[0]!] })).toThrow('defect branch');
    expect(() => validateMutationDefinition({ ...mutationDefinition, branches: [...mutationDefinition.branches, mutationDefinition.branches[0]!] })).toThrow('duplicated');
  });
});

describe('credential resolution', () => {
  const driver: CredentialRefreshDriver<string> = { refreshRequired: () => false, refresh: async () => ({ credentials: 'new', expiresAt: new Date() }) };

  it('resolves static values and does not refresh them', async () => {
    const provider = new StaticCredentialProvider([[call.connection, 'secret']]);
    expect(await resolveCredential(provider, call, driver)).toBe('secret');
    await expect(provider.resolve({ ...call, connection: { provider: 'sample', name: 'missing' } })).rejects.toThrow('not configured');
    await expect(resolveCredentialAfterRejection(provider, call, driver)).rejects.toThrow('does not support');
  });

  it('prefers host refresh and forces one rejection refresh only when supported', async () => {
    const provider = { resolve: async () => 'old', resolveWithRefresh: async () => 'current', resolveAfterRejection: async () => 'replacement' };
    expect(await resolveCredential(provider, call, driver)).toBe('current');
    expect(await resolveCredentialAfterRejection(provider, call, driver)).toBe('replacement');
    await expect(resolveCredential(null as never, call, driver)).rejects.toThrow('provider is required');
    await expect(resolveCredential(provider, call, null as never)).rejects.toThrow('driver is required');
  });
});
