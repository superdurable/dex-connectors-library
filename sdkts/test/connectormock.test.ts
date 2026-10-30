// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { z } from 'zod';
import { branchCase, decodeCallReport, decodeOutput, loadOutputFixture, retryCase, uncertainCase } from '../src/connectormock/index.js';

const failure = { kind: 'TRANSPORT', provider: 'fixture', operation: 'createItem', message: 'outcome unknown' } as const;

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
});
