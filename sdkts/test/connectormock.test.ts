// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { z } from 'zod';
import type { AsyncContext } from '@superdurable/dex';
import { branchCase, decodeCallReport, decodeOutput, loadOutputFixture, newMutation, newQuery, retryCase, uncertainCase } from '../src/connectormock/index.js';
import { createCall } from '../src/credentials.js';
import { RetryError } from '../src/errors.js';
import { runMutation, runQuery } from '../src/run_operation.js';
import type { MutationDefinition } from '../src/operation.js';

const failure = { kind: 'TRANSPORT', provider: 'fixture', operation: 'createItem', message: 'outcome unknown' } as const;
const definition: MutationDefinition = { operation: { connectorId: 'fixture', operationId: 'createItem' }, branches: [{ id: 'created', description: 'Created' }, { id: 'defect', description: 'Connector defect', optional: true }, { id: 'uncertain', description: 'Unknown write', optional: true }], stepDefaults: {} };
function call(flowId: string, stepExecutionId = 'createItem:1') {
  const context = { flowId, stepExecutionId, attempt: 1 } as AsyncContext;
  return createCall(context, { provider: 'fixture', name: 'default' }, definition.operation);
}

describe('connector mock fixtures', () => {
  it('strictly decodes a fixture and models three outcomes', () => {
    const output = decodeOutput('{"id":"item-1"}', z.strictObject({ id: z.string() }));
    expect(output.id).toBe('item-1');
    expect(() => decodeOutput('{"id":"item-1","token":"secret"}', z.strictObject({ id: z.string() }))).toThrow('violates');
    expect(() => decodeOutput('{"id":"item-1","token":"secret"}', z.object({ id: z.string() }))).toThrow('violates');
    expect(branchCase('created', output).kind).toBe('branch');
    expect(retryCase(failure, 500).kind).toBe('retry');
    expect(uncertainCase(output, failure)).toMatchObject({ kind: 'uncertain', branch: 'uncertain' });
  });

  it('parses the Go report field names', () => {
    const report = decodeCallReport('{"calls":[{"connection":"default","operation":"createItem","connectorId":"fixture","branch":"created"}]}');
    expect(report.calls[0]?.connection).toBe('default');
    expect(JSON.parse(JSON.stringify(report))).toEqual(report);
  });

  it('loads and validates a fixture file', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sdkts-mock-'));
    try {
      const path = join(directory, 'output.json');
      await writeFile(path, '{"id":"item-2"}', { mode: 0o600 });
      expect(await loadOutputFixture(path, z.strictObject({ id: z.string() }))).toEqual({ id: 'item-2' });
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  });

  it('selects longest Flow scripts, predicates, queues, and repeating defaults', async () => {
    const query = newQuery<string, string>({ ...definition, branches: definition.branches.filter(branch => branch.id !== 'uncertain') });
    query.default(branchCase('created', 'unscoped'));
    query.forFlow('flow-').default(branchCase('created', 'prefix'));
    query.forFlow('flow-exact').when(input => input === 'special', branchCase('created', 'rule')).respond(branchCase('created', 'queued'));
    expect((await runQuery(query, call('flow-exact'), 'special')).value).toBe('rule');
    expect((await runQuery(query, call('flow-exact', 'createItem:2'), 'plain')).value).toBe('queued');
    expect((await runQuery(query, call('flow-exact', 'createItem:3'), 'plain')).value).toBe('prefix');
    expect((await runQuery(query, call('another'), 'plain')).value).toBe('unscoped');
    query.assertNoFailures();
  });

  it('consumes retry then replays the first terminal mutation outcome', async () => {
    const mutation = newMutation<string, string>(definition).respond(retryCase(failure, 200), branchCase('created', 'item-1'), branchCase('created', 'item-2'));
    const firstCall = call('flow-retry');
    await expect(runMutation(mutation, firstCall, 'request')).rejects.toBeInstanceOf(RetryError);
    expect((await runMutation(mutation, firstCall, 'request')).value).toBe('item-1');
    expect((await runMutation(mutation, firstCall, 'request')).value).toBe('item-1');
    expect(mutation.calls().map(record => [record.retry, record.replay])).toEqual([[true, undefined], [undefined, undefined], [undefined, true]]);
    mutation.assertNoFailures();
  });

  it('records unscripted calls and invalid branch cases as defects', async () => {
    const mutation = newMutation<string, string>(definition).strict();
    const result = await runMutation(mutation, call('flow-unscripted'), 'request');
    expect(result.branch).toBe('defect');
    expect(mutation.calls()[0]?.unscripted).toBe(true);
    expect(() => mutation.assertNoFailures()).toThrow('unscripted call');
    const badBranch = newMutation<string, string>(definition).respond(branchCase('missing', 'item'));
    expect((await runMutation(badBranch, call('flow-bad'), 'request')).branch).toBe('defect');
    expect(() => badBranch.assertNoFailures()).toThrow('not declared');
  });

  it('preserves uncertainty and bypasses the answer check for retries', async () => {
    const mutation = newMutation<string, string>(definition, { answerCheck: { branch: 'created', check: () => { throw new Error('mismatch'); } } });
    mutation.respond(uncertainCase('', failure));
    expect((await runMutation(mutation, call('flow-uncertain'), '')).branch).toBe('uncertain');
    mutation.assertNoFailures();
  });
});
