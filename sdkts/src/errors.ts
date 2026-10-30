// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';

/** Provider or local fact independent of retry policy. */
export const FailureKindSchema = z.enum([
  'VALIDATION', 'AUTHENTICATION', 'AUTHORIZATION', 'NOT_FOUND', 'CONFLICT',
  'RATE_LIMIT', 'QUOTA_EXHAUSTED', 'AVAILABILITY', 'PROVIDER_REJECTION',
  'TRANSPORT', 'RESPONSE_TOO_LARGE', 'PROTOCOL', 'LOCAL_DEFECT',
]);
/** Provider-neutral failure category. */
export type FailureKind = z.infer<typeof FailureKindSchema>;

/** Secret-safe failure that may be persisted in a Flow. */
export const FailureSchema = z.strictObject({ kind: FailureKindSchema, provider: z.string().min(1), operation: z.string().min(1), message: z.string().min(1) });
/** Safe provider failure without secret-bearing details. */
export type Failure = z.infer<typeof FailureSchema>;

/** The only connector error that requests a Dex Step retry. */
export class RetryError extends Error {
  /** Validated provider fact behind the retry request. */
  readonly failure: Failure;

  constructor(failure: Failure) {
    const validated = FailureSchema.parse(failure);
    super(`connector ${validated.provider} ${validated.operation} retry (${validated.kind}): ${validated.message}`);
    this.name = 'RetryError';
    this.failure = validated;
  }
}

/** Terminal credential failure requiring interactive authorization. */
export class ReauthorizationRequiredError extends Error {
  constructor() {
    super('connector reauthorization is required');
    this.name = 'ReauthorizationRequiredError';
  }
}
