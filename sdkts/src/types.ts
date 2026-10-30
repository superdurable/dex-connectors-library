// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { createHash } from 'node:crypto';
import { FailureSchema, type Failure } from './errors.js';

/** Stable UUID for one provider call across Dex retries. */
export const CallIDSchema = z.uuid();
/** Stable UUID identity for one provider call. */
export type CallID = z.infer<typeof CallIDSchema>;

/** UUIDv5 namespace shared with sdkgo for call derivation. */
export const CallIDNamespace = '63281a42-70ad-5bc3-a289-728819af723f';

/** Provider-safe stable deduplication key. */
export type IdempotencyKey = string;

/** Logical credential reference safe to persist in a Flow. */
export const ConnectionRefSchema = z.strictObject({ provider: z.string().refine(value => value.trim().length > 0), name: z.string().refine(value => value.trim().length > 0) });
/** Logical credential identity safe for Flow state. */
export type ConnectionRef = z.infer<typeof ConnectionRefSchema>;

/** Stable manifest identity of a connector operation. */
export const OperationRefSchema = z.strictObject({ connectorId: z.string().regex(/^[a-z][a-z0-9-]{1,62}$/), operationId: z.string().regex(/^[a-z][A-Za-z0-9]+$/) });
/** Manifest operation identity. */
export type OperationRef = z.infer<typeof OperationRefSchema>;

/** Derive the Go-compatible UUIDv5 identity from one Dex Step execution. */
export function deriveCallID(flowId: string, stepExecutionId: string, operation: OperationRef): CallID {
  OperationRefSchema.parse(operation);
  const namespace = CallIDNamespace;
  const fields = [flowId, stepExecutionId, operation.connectorId, operation.operationId];
  const payload = Buffer.concat(fields.flatMap(field => {
    const encoded = Buffer.from(field, 'utf8');
    const length = Buffer.alloc(4);
    length.writeUInt32BE(encoded.length);
    return [length, encoded];
  }));
  return CallIDSchema.parse(uuidSHA1(namespace, payload));
}

/** Provider key fallback for an empty operation-specific idempotency key. */
export function defaultIdempotencyKey(callId: CallID): IdempotencyKey { return callId; }

function uuidSHA1(namespace: string, name: Uint8Array): string {
  const namespaceBytes = Buffer.from(namespace.replaceAll('-', ''), 'hex');
  const digest = createHash('sha1').update(namespaceBytes).update(name).digest();
  digest[6] = ((digest[6] ?? 0) & 0x0f) | 0x50;
  digest[8] = ((digest[8] ?? 0) & 0x3f) | 0x80;
  const hex = digest.subarray(0, 16).toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/** Stable operation-defined process route. */
export type BranchID = string;
/** Standard route for local connector defects. */
export const DefectBranchID: BranchID = 'defect';
/** Optional route for an unsafe, unconfirmable mutation outcome. */
export const UncertainBranchID: BranchID = 'uncertain';

/** One declared operation route; only the happy path is required. */
export const BranchDefinitionSchema = z.strictObject({ id: z.string().min(1), description: z.string(), optional: z.boolean().optional() });
/** One declared operation branch. */
export type BranchDefinition = z.infer<typeof BranchDefinitionSchema>;

/** Go-compatible Step defaults wire form; durations are nanoseconds. */
export const StepDefaultsWireSchema = z.strictObject({
  executeMethodTimeout: z.number().int().nonnegative(), heartbeatTimeout: z.number().int().nonnegative().optional(),
  executeRetry: z.strictObject({ InitialInterval: z.number().int().nonnegative(), BackoffCoefficient: z.number().positive(), MaximumInterval: z.number().int().nonnegative(), MaximumAttempts: z.number().int().nonnegative(), TotalDuration: z.number().int().nonnegative() }).optional(),
  executeDurability: z.number().int(),
});
/** Wire defaults with durations in nanoseconds. */
export type StepDefaultsWire = z.infer<typeof StepDefaultsWireSchema>;

/** Query operation definition shared with connector manifests. */
export const QueryDefinitionWireSchema = z.strictObject({ operation: OperationRefSchema, branches: z.array(BranchDefinitionSchema).nullable(), stepDefaults: StepDefaultsWireSchema });
/** Query definition wire value. */
export type QueryDefinitionWire = z.infer<typeof QueryDefinitionWireSchema>;

/** Mutation operation definition shared with connector manifests. */
export const MutationDefinitionWireSchema = QueryDefinitionWireSchema;
/** Mutation definition wire value. */
export type MutationDefinitionWire = QueryDefinitionWire;

/** Safe provider correlation data, never credential material. */
export const ReceiptSchema = z.strictObject({
  callId: CallIDSchema, idempotencyKey: z.string().optional(), provider: z.string().min(1),
  providerObjectId: z.string().optional(), providerRequestId: z.string().optional(),
  observedAt: z.iso.datetime({ offset: true }), metadata: z.record(z.string(), z.string()).optional(),
});
/** Safe provider correlation receipt. */
export type Receipt = z.infer<typeof ReceiptSchema>;

/** Validated terminal query result with Go-compatible JSON field names. */
export const QueryResultSchema = <T>(valueSchema: z.ZodType<T>) => z.strictObject({ branch: z.string().min(1), value: valueSchema, receipt: ReceiptSchema, failure: FailureSchema.optional() });
/** Terminal query value and its selected branch. */
export type QueryResult<T> = { branch: BranchID; value: T; receipt: Receipt; failure?: Failure };

/** Validated terminal mutation result with Go-compatible JSON field names. */
export const MutationResultSchema = QueryResultSchema;
/** Terminal mutation value and its selected branch. */
export type MutationResult<T> = QueryResult<T>;

/** One query invocation outcome; only constructors create valid attempts. */
export type QueryAttempt<T> =
  | { readonly kind: 'branch'; readonly branch: BranchID; readonly value: T; readonly receipt: Receipt; readonly failure?: Failure }
  | { readonly kind: 'retry'; readonly failure: Failure; readonly retryAfterMs: number };

/** One mutation invocation outcome; uncertainty is reserved for unsafe, unconfirmable writes. */
export type MutationAttempt<T> = QueryAttempt<T> | { readonly kind: 'uncertain'; readonly value: T; readonly receipt: Receipt; readonly failure: Failure };

/** Construct a terminal query branch. */
export function newQueryBranch<T>(branch: BranchID, value: T, receipt: Receipt, failure?: Failure): QueryAttempt<T> {
  return failure === undefined ? { kind: 'branch', branch, value, receipt } : { kind: 'branch', branch, value, receipt, failure };
}

/** Construct a retryable query failure with an optional provider delay in milliseconds. */
export function newQueryRetry<T>(failure: Failure, retryAfterMs = 0): QueryAttempt<T> {
  validateRetryDelay(retryAfterMs);
  return { kind: 'retry', failure: FailureSchema.parse(failure), retryAfterMs };
}

/** Construct a terminal mutation branch. */
export function newMutationBranch<T>(branch: BranchID, value: T, receipt: Receipt, failure?: Failure): MutationAttempt<T> {
  return newQueryBranch(branch, value, receipt, failure);
}

/** Construct a retryable mutation failure when repeating the effect is safe. */
export function newMutationRetry<T>(failure: Failure, retryAfterMs = 0): MutationAttempt<T> {
  return newQueryRetry(failure, retryAfterMs);
}

/** Construct an uncertain mutation only after proving repeat and read-back are unsafe. */
export function newMutationUncertain<T>(value: T, failure: Failure, receipt: Receipt): MutationAttempt<T> {
  return { kind: 'uncertain', value, failure: FailureSchema.parse(failure), receipt: ReceiptSchema.parse(receipt) };
}

/** Dex Execute retry policy using millisecond durations. */
export interface RetryPolicy {
  /** Delay before the first retry. */ initialIntervalMs: number;
  /** Multiplier applied between attempts. */ backoffCoefficient: number;
  /** Maximum delay between attempts. */ maximumIntervalMs: number;
  /** Zero means unlimited attempts within total duration. */ maximumAttempts: number;
  /** Overall bound including the first attempt. */ totalDurationMs: number;
}

/** Return a fresh one-hour connector retry policy. */
export function defaultConnectorStepExecuteRetryPolicy(): RetryPolicy {
  return { initialIntervalMs: 1000, backoffCoefficient: 2, maximumIntervalMs: 300000, maximumAttempts: 0, totalDurationMs: 3600000 };
}

/** Encode the retry policy in Dex Go's nanosecond JSON wire form. */
export function retryPolicyToGoWire(policy: RetryPolicy): Record<string, number> {
  return {
    InitialInterval: policy.initialIntervalMs * 1_000_000,
    BackoffCoefficient: policy.backoffCoefficient,
    MaximumInterval: policy.maximumIntervalMs * 1_000_000,
    MaximumAttempts: policy.maximumAttempts,
    TotalDuration: policy.totalDurationMs * 1_000_000,
  };
}

/** Connector-owned UI binding from a unit port to configuration JSON. */
export const ConnectorUIBindingSchema = z.strictObject({ port: z.string().regex(/^[a-z][A-Za-z0-9]*$/), jsonPointer: z.string().regex(/^\/(?:[^~]|~[01])*$/) });
/** Bind a UI port to a configuration JSON pointer. */
export type ConnectorUIBinding = z.infer<typeof ConnectorUIBindingSchema>;

/** Reusable connector UI unit with contextual guidance and ordered bindings. */
export const ConnectorUIUnitSchema = z.strictObject({
  id: z.string().regex(/^[a-z][A-Za-z0-9]*$/), unitId: z.string().regex(/^[a-z][A-Za-z0-9]*$/),
  label: z.string().refine(value => value.trim().length > 0), description: z.string().refine(value => value.trim().length > 0), required: z.boolean(),
  bindings: z.array(ConnectorUIBindingSchema).min(1),
}).superRefine((unit, context) => {
  if (new Set(unit.bindings.map(binding => binding.port)).size !== unit.bindings.length) context.addIssue({ code: 'custom', message: 'duplicate UI port' });
});
/** Contextual connector UI unit. */
export type ConnectorUIUnit = z.infer<typeof ConnectorUIUnitSchema>;

/** Ordered connector-owned UI units composed by Dex Web. */
export const ConnectorConfigurationUISchema = z.strictObject({ units: z.array(ConnectorUIUnitSchema) }).superRefine((ui, context) => {
  if (new Set(ui.units.map(unit => unit.id)).size !== ui.units.length) context.addIssue({ code: 'custom', message: 'duplicate UI unit ID' });
});
/** Ordered connector UI configuration. */
export type ConnectorConfigurationUI = z.infer<typeof ConnectorConfigurationUISchema>;

/** Stable Flow and Step use that owns one operation configuration. */
export const ConnectorConfigurationRefSchema = z.strictObject({
  connectorId: OperationRefSchema.shape.connectorId, connectionName: z.string().refine(value => value.trim().length > 0),
  operationId: OperationRefSchema.shape.operationId, flowType: z.string().refine(value => value.trim().length > 0), stepType: z.string().refine(value => value.trim().length > 0),
});
/** Stable Flow and Step configuration owner. */
export type ConnectorConfigurationRef = z.infer<typeof ConnectorConfigurationRefSchema>;

function validateRetryDelay(delayMs: number): void {
  if (!Number.isSafeInteger(delayMs) || delayMs < 0) throw new RangeError('retry delay must be a non-negative integer in milliseconds');
}
