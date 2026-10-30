// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError } from '../src/errors.js';
import { createCall, StaticCredentialProvider, resolveCredential, resolveCredentialAfterRejection, type CredentialRefreshDriver } from '../src/credentials.js';
import { validateMutationDefinition, validateQueryDefinition, type MutationDefinition, type QueryDefinition } from '../src/operation.js';
import { runMutation, runQuery } from '../src/run_operation.js';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt } from '../src/types.js';
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
