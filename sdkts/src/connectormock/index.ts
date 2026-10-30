// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile } from 'node:fs/promises';
import { isDeepStrictEqual } from 'node:util';
import { z } from 'zod';
import { FailureSchema, type Failure } from '../errors.js';
import { type BranchID, type CallID, UncertainBranchID } from '../types.js';
export { FlowScript, MockQuery, MockMutation, newQuery, newMutation, type MockCall, type MockOptions } from './scripted.js';

/** Environment variable that enables the test harness call report. */
export const ReportDirectoryEnvironment = 'DEX_CONNECTOR_MOCK_REPORT_DIR';
/** Report file consumed by the connector test harness. */
export const ReportFileName = 'connector-calls.json';

/** One scripted provider outcome independent of a Dex Step. */
export type Case<T> =
  | { readonly kind: 'branch'; readonly branch: BranchID; readonly value: T; readonly failure?: Failure }
  | { readonly kind: 'retry'; readonly failure: Failure; readonly retryAfterMs: number }
  | { readonly kind: 'uncertain'; readonly branch: typeof UncertainBranchID; readonly value: T; readonly failure: Failure };

/** Construct a terminal branch case. */
export function branchCase<T>(branch: BranchID, value: T, failure?: Failure): Case<T> {
  if (!branch) throw new TypeError('mock branch is required');
  return failure ? { kind: 'branch', branch, value, failure: FailureSchema.parse(failure) } : { kind: 'branch', branch, value };
}

/** Construct a retry case; another scripted case answers the next attempt. */
export function retryCase<T>(failure: Failure, retryAfterMs = 0): Case<T> {
  if (!Number.isSafeInteger(retryAfterMs) || retryAfterMs < 0) throw new RangeError('mock retry delay is invalid');
  return { kind: 'retry', failure: FailureSchema.parse(failure), retryAfterMs };
}

/** Construct an unsafe, unconfirmable mutation case. */
export function uncertainCase<T>(value: T, failure: Failure): Case<T> {
  return { kind: 'uncertain', branch: UncertainBranchID, value, failure: FailureSchema.parse(failure) };
}

/** One provider call answered by a mock, with Go-compatible report fields. */
export const RecordedCallSchema = z.strictObject({
  connectorId: z.string().min(1), operationId: z.string().min(1), connectionName: z.string().min(1),
  flowId: z.string(), callId: z.uuid(), branch: z.string().optional(),
  retry: z.boolean().optional(), replay: z.boolean().optional(), unscripted: z.boolean().optional(),
});
/** One fixture-backed provider call. */
export type RecordedCall = z.infer<typeof RecordedCallSchema> & { callId: CallID };

/** Wire format of the mock call report. */
export const ReportRowSchema = z.strictObject({
  connection: z.string(), operation: z.string(), branch: z.string().optional(), connectorId: z.string(),
  flowId: z.string().optional(), callId: z.string().optional(), retry: z.boolean().optional(),
  replay: z.boolean().optional(), unscripted: z.boolean().optional(),
});
/** Wire format of the complete mock call report. */
export const CallReportSchema = z.strictObject({ calls: z.array(ReportRowSchema) });
/** Safe report of connector mock calls. */
export type CallReport = z.infer<typeof CallReportSchema>;

/** Decode one strict JSON fixture and validate it against the output schema. */
export function decodeOutput<T>(encoded: string, outputSchema: z.ZodType<T>): T {
  let document: unknown;
  try { document = JSON.parse(encoded) as unknown; }
  catch { throw new TypeError('mock output fixture is not valid JSON'); }
  const parsed = outputSchema.safeParse(document);
  if (!parsed.success || !isDeepStrictEqual(parsed.data, document)) throw new TypeError('mock output fixture violates the output schema');
  return parsed.data;
}

/** Load a private test fixture from disk and validate its complete shape. */
export async function loadOutputFixture<T>(path: string, outputSchema: z.ZodType<T>): Promise<T> {
  return decodeOutput(await readFile(path, 'utf8'), outputSchema);
}

/** Decode a Go-compatible call report without exposing test input values. */
export function decodeCallReport(encoded: string): CallReport {
  let document: unknown;
  try { document = JSON.parse(encoded) as unknown; }
  catch { throw new TypeError('connector call report is not valid JSON'); }
  return CallReportSchema.parse(document);
}
