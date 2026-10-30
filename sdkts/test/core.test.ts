// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { z } from 'zod';
import {
  CallIDSchema, ConnectorConfigurationUISchema, QueryResultSchema, ReceiptSchema,
  defaultConnectorStepExecuteRetryPolicy, newMutationRetry, newMutationUncertain, newQueryBranch,
} from '../src/types.js';
import { FailureSchema, ReauthorizationRequiredError, RetryError } from '../src/errors.js';
import { SecretString } from '../src/credentials.js';

const failure = { kind: 'AVAILABILITY', provider: 'fixture', operation: 'readItem', message: 'temporarily unavailable' } as const;
const receipt = { callId: '6750e62f-4d15-4e7e-a9d3-ec90f4f17b35', provider: 'fixture', observedAt: '2026-10-29T12:00:00Z' };

describe('core contracts', () => {
  it('validates and round-trips Go-compatible result JSON', () => {
    expect(CallIDSchema.parse(receipt.callId)).toBe(receipt.callId);
    const result = QueryResultSchema(z.object({ count: z.number().int() })).parse({ branch: 'found', value: { count: 2 }, receipt });
    expect(JSON.parse(JSON.stringify(result))).toEqual(result);
    expect(ReceiptSchema.safeParse({ ...receipt, token: 'secret' }).success).toBe(false);
    expect(newQueryBranch('found', result.value, result.receipt)).toMatchObject({ kind: 'branch', branch: 'found' });
  });

  it('keeps retry separate from uncertainty', () => {
    const retry = newMutationRetry(failure, 1500);
    expect(retry).toMatchObject({ kind: 'retry', retryAfterMs: 1500 });
    expect(newMutationUncertain({}, failure, ReceiptSchema.parse(receipt)).kind).toBe('uncertain');
    expect(() => newMutationRetry(failure, -1)).toThrow(RangeError);
    expect(defaultConnectorStepExecuteRetryPolicy()).toEqual({ initialIntervalMs: 1000, backoffCoefficient: 2, maximumIntervalMs: 300000, maximumAttempts: 0, totalDurationMs: 3600000 });
  });

  it('rejects bad failures and duplicate UI bindings', () => {
    expect(FailureSchema.safeParse({ ...failure, kind: 'UNKNOWN' }).success).toBe(false);
    expect(new RetryError(failure).message).toContain('AVAILABILITY');
    expect(new ReauthorizationRequiredError().message).toBe('connector reauthorization is required');
    const unit = { id: 'account', unitId: 'accountPicker', label: 'Account', description: 'Choose the account.', required: true, bindings: [{ port: 'value', jsonPointer: '/accountId' }] };
    expect(ConnectorConfigurationUISchema.safeParse({ units: [unit] }).success).toBe(true);
    expect(ConnectorConfigurationUISchema.safeParse({ units: [unit, unit] }).success).toBe(false);
  });

  it('never prints or serializes a secret wrapper', () => {
    const secret = new SecretString('private-token');
    expect(String(secret)).toBe('[REDACTED]');
    expect(() => JSON.stringify(secret)).toThrow('cannot be serialized');
    expect(secret.reveal()).toBe('private-token');
  });
});
