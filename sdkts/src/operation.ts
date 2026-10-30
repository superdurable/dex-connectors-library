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

/** Stable query routes and Execute defaults. */
export interface QueryDefinition {
  /** Manifest operation identity. */ operation: OperationRef;
  /** Declared routes, including the standard defect route. */ branches: readonly BranchDefinition[];
  /** Execute options for the generated Step. */ stepDefaults: StepDefaults;
}

/** Stable mutation routes and Execute defaults. */
export type MutationDefinition = QueryDefinition;

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
export function validateMutationDefinition(definition: MutationDefinition): void { validateDefinition(definition); }

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
