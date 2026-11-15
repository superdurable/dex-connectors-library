// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call } from './credentials.js';
import type { BranchDefinition, MutationAttempt, OperationRef, QueryAttempt, RetryPolicy } from './types.js';
import { DefectBranchID, OperationRefSchema, UncertainBranchID } from './types.js';

/** Execute options owned by one operation; durations are milliseconds. */
export interface StepDefaults {
  /** Zero or absence uses the Dex method timeout. */ executeMethodTimeoutMs?: number;
  /** Zero or absence uses the Dex heartbeat timeout. */ heartbeatTimeoutMs?: number;
  /** Absence uses the connector one-hour retry policy. */ executeRetry?: RetryPolicy;
  /** Absence inherits the Flow durability. */ executeDurability?: 'default' | 'sync' | 'async';
}

/**
 * Manifest `execution.repeatOnUnknown`: what an operation may do when a request's outcome is unknown.
 *
 * - `safe`: repeating is harmless, so the Step retry policy retries it.
 * - `limited`: a duplicate is acceptable, so the connector reads back when it can and resends while
 *   the possible sends stay below the operation's send cap.
 * - `unsafe`: a duplicate does real harm, so the connector reads back only and never resends.
 */
export type RepeatOnUnknown = 'safe' | 'limited' | 'unsafe';

/** Send cap of a `limited` operation whose manifest omits `execution.maxSends`. */
export const DefaultLimitedMaxSends = 3;
/** Largest send cap a manifest or an application override may set. */
export const MaximumMaxSends = 10;

/** Stable query routes and Execute defaults. */
export interface QueryDefinition {
  /** Manifest operation identity. */ operation: OperationRef;
  /** Declared routes, including the standard defect route. */ branches: readonly BranchDefinition[];
  /** Execute options for the generated Step. */ stepDefaults: StepDefaults;
}

/** Stable mutation routes, Execute defaults, and unknown-outcome policy. A Query is always `safe`. */
export interface MutationDefinition extends QueryDefinition {
  /**
   * Manifest `execution.repeatOnUnknown`. Absence means the definition predates the field, and a
   * Step factory then rejects an application send cap.
   */
  repeatOnUnknown?: RepeatOnUnknown;
  /**
   * Manifest `execution.maxSends`, an integer from 2 to 10, allowed only with `limited`. Absence
   * means {@link DefaultLimitedMaxSends}. It caps possibly effective sends, not Execute attempts.
   */
  maxSends?: number;
}

/** Whether a value is an integer send cap from 1 to {@link MaximumMaxSends}. */
export function isValidMaxSends(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= MaximumMaxSends;
}

/** Send cap that a definition declares: its `maxSends` or the default when `limited`, otherwise 1. */
export function definitionMaxSends(definition: MutationDefinition): number {
  return definition.repeatOnUnknown === 'limited' ? definition.maxSends ?? DefaultLimitedMaxSends : 1;
}

/** Read-only provider operation invoked once per Dex Execute attempt. */
export interface Query<Input, Output> {
  /** Return an immutable operation definition. */ definition(): QueryDefinition;
  /** Classify one provider call as a branch or safe retry. */ invoke(call: Call, input: Input): QueryAttempt<Output> | Promise<QueryAttempt<Output>>;
}

/** Provider write with a stable deduplication key and classified attempt. */
export interface Mutation<Input, Output> {
  /** Return an immutable operation definition. */ definition(): MutationDefinition;
  /** Derive a provider-safe key from the stable call ID and input. */ idempotencyKey(callId: string, input: Input): string;
  /** Classify one provider call as a branch, safe retry, or uncertainty. */ invoke(call: Call, input: Input): MutationAttempt<Output> | Promise<MutationAttempt<Output>>;
}

/** Validate a query definition before any provider call or Step registration. */
export function validateQueryDefinition(definition: QueryDefinition): void {
  validateDefinition(definition);
  if (definition.branches.some(branch => branch.id === UncertainBranchID)) throw new TypeError('query cannot declare the uncertain branch');
}

/** Validate a mutation definition before any provider call or Step registration. */
export function validateMutationDefinition(definition: MutationDefinition): void {
  validateDefinition(definition);
  if (definition.repeatOnUnknown !== undefined && !['safe', 'limited', 'unsafe'].includes(definition.repeatOnUnknown)) throw new TypeError('mutation repeatOnUnknown must be safe, limited, or unsafe');
  if (definition.maxSends === undefined) return;
  if (definition.repeatOnUnknown !== 'limited') throw new TypeError('mutation maxSends requires repeatOnUnknown limited');
  if (!isValidMaxSends(definition.maxSends) || definition.maxSends < 2) throw new TypeError(`mutation maxSends must be an integer from 2 to ${MaximumMaxSends}`);
}

function validateDefinition(definition: QueryDefinition): void {
  OperationRefSchema.parse(definition.operation);
  if (definition.branches.length === 0) throw new TypeError('at least one branch is required');
  const branchIds = new Set<string>();
  for (const branch of definition.branches) {
    if (!/^[a-z][A-Za-z0-9]+$/.test(branch.id)) throw new TypeError(`branch ID ${branch.id} must be lower camel case`);
    if (branchIds.has(branch.id)) throw new TypeError(`branch ID ${branch.id} is duplicated`);
    if (!branch.description.trim()) throw new TypeError(`branch ${branch.id} description is required`);
    branchIds.add(branch.id);
  }
  if (!branchIds.has(DefectBranchID)) throw new TypeError('defect branch is not declared');
  const defaults = definition.stepDefaults;
  for (const duration of [defaults.executeMethodTimeoutMs, defaults.heartbeatTimeoutMs]) {
    if (duration !== undefined && (!Number.isFinite(duration) || duration < 0)) throw new TypeError('Step timeouts cannot be negative');
  }
  if (defaults.executeDurability !== undefined && !['default', 'sync', 'async'].includes(defaults.executeDurability)) throw new TypeError('Step Execute durability is invalid');
  if (defaults.executeRetry !== undefined) {
    const retry = defaults.executeRetry;
    for (const value of [retry.initialIntervalMs, retry.maximumIntervalMs, retry.totalDurationMs, retry.maximumAttempts, retry.backoffCoefficient]) {
      if (!Number.isFinite(value) || value < 0) throw new TypeError('Step retry fields cannot be negative');
    }
  }
}
