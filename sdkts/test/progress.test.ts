// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { AsyncContext, Stream } from '@superdurable/dex';
import { createCall, type ProgressUpdate } from '../src/credentials.js';
import { runMutation } from '../src/run_operation.js';
import { newMutationBranch } from '../src/types.js';

const context = { flowId: 'progress-flow', stepExecutionId: 'create:1', attempt: 2 } as AsyncContext;
const connection = { provider: 'fixture', name: 'default' };
const operation = { connectorId: 'fixture', operationId: 'createItem' };

describe('connector progress', () => {
  it('is a no-op without configured Streams', () => {
    const call = createCall(context, connection, operation);
    expect(call.hasProgressStream()).toBe(false);
    expect(call.hasTextStream()).toBe(false);
    expect(() => call.reportProgress({ phase: 'started', percent: 2 })).not.toThrow();
    expect(() => call.writeText('hello')).not.toThrow();
    expect(JSON.stringify(call)).not.toContain('reportProgress');
  });

  it('adds stable call identity and monotonic sequence to each progress update', () => {
    const updates: ProgressUpdate[] = [];
    const progressStream = { write: (_context: AsyncContext, update: ProgressUpdate) => updates.push(update) } as unknown as Stream<ProgressUpdate>;
    const call = createCall(context, connection, operation, { progressStream });
    expect(call.hasProgressStream()).toBe(true);
    call.reportProgress({ phase: 'started', percent: 0, providerSequence: 7 });
    call.reportProgress({ phase: 'finished', message: 'done', percent: 1 });
    expect(updates).toEqual([
      { phase: 'started', percent: 0, providerSequence: 7, callId: call.id, attempt: 2, sequence: 1 },
      { phase: 'finished', message: 'done', percent: 1, callId: call.id, attempt: 2, sequence: 2 },
    ]);
    expect(() => call.reportProgress({ phase: 'bad', percent: Number.NaN })).toThrow(RangeError);
    expect(() => call.reportProgress({ phase: 'bad', percent: -0.1 })).toThrow(RangeError);
    expect(() => call.reportProgress({ phase: 'bad', percent: 1.1 })).toThrow(RangeError);
    expect(updates).toHaveLength(2);
  });

  it('keeps progress and buffered text on the mutation call with its stable key', async () => {
    const updates: ProgressUpdate[] = [];
    const chunks: string[] = [];
    const progressStream = { write: (_context: AsyncContext, update: ProgressUpdate) => updates.push(update) } as unknown as Stream<ProgressUpdate>;
    const textStream = { bufferedText: (_context: AsyncContext, options: object) => {
      expect(options).toEqual({ maxBufferedBytes: 1024 });
      return { write: (chunk: string) => chunks.push(chunk) };
    } } as unknown as Stream<string>;
    const call = createCall(context, connection, operation, { progressStream, textStream, textOptions: { maxBufferedBytes: 1024 } });
    const result = await runMutation({
      definition: () => ({ operation, branches: [{ id: 'created', description: 'Created' }, { id: 'defect', description: 'Defect', optional: true }], stepDefaults: {} }),
      idempotencyKey: callId => `fixture/${callId}`,
      invoke: receivedCall => {
        expect(receivedCall.hasProgressStream()).toBe(true);
        expect(receivedCall.hasTextStream()).toBe(true);
        expect(receivedCall.idempotencyKey).toBe(`fixture/${call.id}`);
        receivedCall.reportProgress({ phase: 'dispatching' });
        receivedCall.writeText('first');
        receivedCall.writeText(' second');
        expect(JSON.stringify(receivedCall)).not.toContain('reportProgress');
        return newMutationBranch('created', { id: 'item-1' });
      },
    }, call, undefined);
    expect(result.branch).toBe('created');
    expect(updates).toEqual([{ phase: 'dispatching', callId: call.id, attempt: 2, sequence: 1 }]);
    expect(chunks).toEqual(['first', ' second']);
  });
});
