// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { RetryAfterError, StepRef, type AsyncContext, type StepOptions } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry } from '../src/types.js';
import { RetryError } from '../src/errors.js';
import { definitionMaxSends, validateMutationDefinition, type MutationDefinition } from '../src/operation.js';
import * as connectorsSDK from '../src/index.js';
import { goToBranch, newMutationStep, newQueryStep, throwDexRetry, type MutationStepConfig, type QueryStepConfig } from '../src/step.js';

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

  it('exports the retry translation that the Step factories use', () => {
    expect(connectorsSDK.throwDexRetry).toBe(throwDexRetry);
    for (const [delay, expected] of [[0, undefined], [500, undefined], [1000, 1], [1900, 1]] as const) {
      const retry = new RetryError(failure, delay);
      try { throwDexRetry(retry); throw new Error('retry was not thrown'); }
      catch (error) {
        if (expected === undefined) expect(error).toBe(retry);
        else { expect(error).toBeInstanceOf(RetryAfterError); expect((error as RetryAfterError).afterSeconds).toBe(expected); expect((error as RetryAfterError).cause).toBe(retry); }
      }
    }
    const unrelated = new TypeError('not a retry');
    expect(() => throwDexRetry(unrelated)).toThrow(unrelated);
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

  it('accepts an application send cap only for a limited Mutation and passes it to every call', async () => {
    const limitedRetry = { initialIntervalMs: 1000, backoffCoefficient: 2, maximumIntervalMs: 60000, maximumAttempts: 4, totalDurationMs: 600000 };
    const observedCaps: (number | undefined)[] = [];
    function mutationConfig(definition: MutationDefinition, maxSends?: number, stepOptionsOverride?: StepOptions): MutationStepConfig<{ id: string }, { id: string }, { id: string }> {
      return { stepType: 'CreateItem', annotations, connection, mapToOperationInput: input => input,
        operation: { definition: () => definition, idempotencyKey: callId => callId, invoke: call => { observedCaps.push(call.maxSendsOverride); return newMutationBranch('created', { id: 'item-1' }); } },
        branches: [goToBranch('created', new StepRef('Created'))],
        ...(maxSends === undefined ? {} : { maxSends }), ...(stepOptionsOverride ? { stepOptionsOverride } : {}) };
    }
    const limited: MutationDefinition = { ...mutationDefinition, repeatOnUnknown: 'limited', maxSends: 3, stepDefaults: { executeRetry: limitedRetry } };

    const widened = newMutationStep(mutationConfig(limited, 5));
    expect(widened.getStepOptions().executeRetry?.maximumAttempts).toBe(6);
    await widened.execute(context, { id: 'item-1' });
    await widened.execute({ ...context, attempt: 2 } as AsyncContext, { id: 'item-1' });
    expect(observedCaps).toEqual([5, 5]);
    observedCaps.length = 0;
    await newMutationStep(mutationConfig(limited)).execute(context, { id: 'item-1' });
    expect(observedCaps).toEqual([undefined]);

    expect(newMutationStep(mutationConfig(limited, 1)).getStepOptions().executeRetry?.maximumAttempts).toBe(4);
    const defaultCap: MutationDefinition = { ...mutationDefinition, repeatOnUnknown: 'limited', stepDefaults: { executeRetry: limitedRetry } };
    expect(newMutationStep(mutationConfig(defaultCap, 5)).getStepOptions().executeRetry?.maximumAttempts).toBe(6);
    const explicitRetry = { ...limitedRetry, maximumAttempts: 2 };
    expect(newMutationStep(mutationConfig(limited, 5, { executeRetry: explicitRetry })).getStepOptions().executeRetry?.maximumAttempts).toBe(2);
    const unlimited: MutationDefinition = { ...mutationDefinition, repeatOnUnknown: 'limited' };
    expect(newMutationStep(mutationConfig(unlimited, 10)).getStepOptions().executeRetry?.maximumAttempts).toBe(0);

    for (const repeatOnUnknown of ['safe', 'unsafe', undefined] as const) {
      const definition: MutationDefinition = repeatOnUnknown ? { ...mutationDefinition, repeatOnUnknown } : mutationDefinition;
      expect(() => newMutationStep(mutationConfig(definition, 2))).toThrow('not limited');
      expect(() => newMutationStep(mutationConfig(definition))).not.toThrow();
    }
    for (const maxSends of [0, 11, 2.5]) expect(() => newMutationStep(mutationConfig(limited, maxSends))).toThrow(RangeError);
    expect(() => newQueryStep({ ...queryConfig(() => newQueryBranch('found', { id: 'item-1' })), maxSends: 2 } as QueryStepConfig<{ id: string }, { id: string }, { id: string }>)).toThrow('maxSends');
  });

  it('validates the unknown-outcome policy of a Mutation definition', () => {
    expect(() => validateMutationDefinition({ ...mutationDefinition, repeatOnUnknown: 'limited', maxSends: 10 })).not.toThrow();
    expect(() => validateMutationDefinition({ ...mutationDefinition, repeatOnUnknown: 'limited', maxSends: 1 })).toThrow('from 2 to 10');
    expect(() => validateMutationDefinition({ ...mutationDefinition, repeatOnUnknown: 'limited', maxSends: 11 })).toThrow('from 2 to 10');
    expect(() => validateMutationDefinition({ ...mutationDefinition, repeatOnUnknown: 'unsafe', maxSends: 3 })).toThrow('requires repeatOnUnknown limited');
    expect(() => validateMutationDefinition({ ...mutationDefinition, maxSends: 3 })).toThrow('requires repeatOnUnknown limited');
    expect(() => validateMutationDefinition({ ...mutationDefinition, repeatOnUnknown: 'sometimes' } as unknown as MutationDefinition)).toThrow('safe, limited, or unsafe');
    expect(definitionMaxSends({ ...mutationDefinition, repeatOnUnknown: 'limited' })).toBe(3);
    expect(definitionMaxSends({ ...mutationDefinition, repeatOnUnknown: 'unsafe' })).toBe(1);
  });
});
