// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { RetryAfterError, StepRef, type AsyncContext, type StepOptions } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry } from '../src/types.js';
import { goToBranch, newMutationStep, newQueryStep, type MutationStepConfig, type QueryStepConfig } from '../src/step.js';

const failure = { kind: 'AVAILABILITY' as const, provider: 'fixture', operation: 'getItem', message: 'temporarily unavailable' };
const context = { flowId: 'flow-1', stepExecutionId: 'query:1', attempt: 1, cancellationSignal: new AbortController().signal } as AsyncContext;
const annotations = { groupId: 'fixture', groupLabel: 'Fixture', explanation: 'Reads an item.' };
const connection = { provider: 'fixture', name: 'default' };
const queryDefinition = { operation: { connectorId: 'fixture', operationId: 'getItem' }, branches: [{ id: 'found', description: 'Found' }, { id: 'defect', description: 'Defect', optional: true }], stepDefaults: {} };
const mutationDefinition = { operation: { connectorId: 'fixture', operationId: 'createItem' }, branches: [{ id: 'created', description: 'Created' }, { id: 'uncertain', description: 'Unknown effect', optional: true }, { id: 'defect', description: 'Defect', optional: true }], stepDefaults: {} };

function queryConfig(invoke: () => ReturnType<typeof newQueryBranch<{ id: string }>> | ReturnType<typeof newQueryRetry<{ id: string }>>): QueryStepConfig<{ id: string }, { id: string }, { id: string }> {
  return { stepType: 'ReadItem', annotations, connection, mapToOperationInput: input => input,
    operation: { definition: () => queryDefinition, invoke }, branches: [goToBranch('found', new StepRef('Found'))] };
}

describe('connector Step factories', () => {
  it('routes a Query result to a StepRef and supplies connector retry defaults', async () => {
    const step = newQueryStep(queryConfig(() => newQueryBranch('found', { id: 'item-1' })));
    expect(step.getStepType()).toBe('ReadItem');
    expect(step.getStepOptions().executeRetry).toEqual({ initialIntervalMs: 1000, backoffCoefficient: 2, maximumIntervalMs: 300000, maximumAttempts: 0, totalDurationMs: 3600000 });
    const decision = await step.execute(context, { id: 'item-1' });
    expect(decision.kind).toBe('next');
    if (decision.kind === 'next') {
      expect(decision.movements[0]?.step).toBeInstanceOf(StepRef);
      expect(decision.movements[0]?.input).toMatchObject({ branch: 'found', value: { id: 'item-1' } });
    }
  });

  it('floors provider retry hints to whole seconds and leaves subsecond hints to policy', async () => {
    for (const [delay, expected] of [[500, undefined], [1900, 1], [2000, 2]] as const) {
      const step = newQueryStep(queryConfig(() => newQueryRetry(failure, delay)));
      try { await step.execute(context, { id: 'item-1' }); throw new Error('retry was not requested'); }
      catch (error) {
        if (expected === undefined) expect(error).not.toBeInstanceOf(RetryAfterError);
        else { expect(error).toBeInstanceOf(RetryAfterError); expect((error as RetryAfterError).afterSeconds).toBe(expected); }
      }
    }
  });

  it('routes a definite Mutation branch and force-fails unwired uncertainty', async () => {
    const config: MutationStepConfig<{ id: string }, { id: string }, { id: string }> = {
      stepType: 'CreateItem', annotations, connection, mapToOperationInput: input => input,
      operation: { definition: () => mutationDefinition, idempotencyKey: callId => callId, invoke: () => newMutationBranch('created', { id: 'item-1' }) },
      branches: [goToBranch('created', new StepRef('Created'))],
    };
    const step = newMutationStep(config);
    expect((await step.execute(context, { id: 'item-1' })).kind).toBe('next');
    const uncertain = newMutationStep<{ id: string }, { id: string }, { id: string }>({ ...config, operation: { ...config.operation, invoke: () => newMutationUncertain({ id: 'item-1' }, failure) } });
    const decision = await uncertain.execute(context, { id: 'item-1' });
    expect(decision).toMatchObject({ kind: 'forceFail', reason: expect.stringContaining('selected branch uncertain with no target') });
    const retry = newMutationStep<{ id: string }, { id: string }, { id: string }>({ ...config, operation: { ...config.operation, invoke: () => newMutationRetry(failure, 2000) } });
    await expect(retry.execute(context, { id: 'item-1' })).rejects.toBeInstanceOf(RetryAfterError);
  });

  it('rejects missing required branch targets and WaitFor overrides', () => {
    expect(() => newQueryStep({ ...queryConfig(() => newQueryBranch('found', { id: 'item-1' })), branches: [] })).toThrow('required');
    const options: StepOptions = { waitForMethodTimeoutMs: 1000 };
    expect(() => newQueryStep({ ...queryConfig(() => newQueryBranch('found', { id: 'item-1' })), stepOptionsOverride: options })).toThrow('WaitFor');
  });
});
