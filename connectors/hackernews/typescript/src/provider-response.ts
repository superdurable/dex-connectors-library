// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, type Failure, type QueryAttempt } from '@superdurable/connectors-sdk';

export function parseProviderJSON(body: Uint8Array): unknown {
  const exactJSON = JSON as typeof JSON & { rawJSON(source: string): { rawJSON: string } };
  return exactJSON.parse(Buffer.from(body).toString('utf8'), (_key, value: unknown, context?: { source?: string }) =>
    typeof value === 'number' && context?.source ? exactJSON.rawJSON(context.source) : value) as unknown;
}

export function strictInteger(value: unknown, name: string): number {
  const exactJSON = JSON as typeof JSON & { isRawJSON(input: unknown): input is { rawJSON: string } };
  const source = exactJSON.isRawJSON(value) ? value.rawJSON : '';
  if (!/^-?(?:0|[1-9]\d*)$/.test(source)) throw new TypeError(name);
  const integer = Number(source);
  if (!Number.isSafeInteger(integer)) throw new TypeError(name);
  return integer;
}

export function fieldError(error: unknown): string { return error instanceof Error ? error.message : 'unknown field'; }

export function goTimestamp(time: Date): string {
  return time.toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*?[1-9])0+Z$/, '$1Z');
}

export function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'hacker-news', operation, message }; }
export function failed<Output>(operation: string, branch: string, zero: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
  return newQueryBranch(branch, zero, {}, failure(operation, kind, message));
}
