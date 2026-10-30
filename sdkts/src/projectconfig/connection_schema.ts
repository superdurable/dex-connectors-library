// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { RFC3339NanoSchema, ScopeSchema } from './model.js';

/** Private connection head shared with the Go project store. */
export const ConnectionRecordSchema = z.strictObject({
  schemaVersion: z.literal('connectors.dex.dev/project-connection/v1alpha2'),
  connection: z.strictObject({ connectorId: z.string(), connectionName: z.string() }),
  scope: ScopeSchema, revision: z.number().int().positive(), fence: z.number().int().positive(),
  status: z.enum(['READY', 'AUTHORIZING', 'REFRESHING', 'REAUTHORIZATION_REQUIRED', 'REVOKED']),
  authMethod: z.string(), expiresAt: RFC3339NanoSchema.nullable().optional(),
  credential: z.strictObject({ key: z.string(), version: z.string(), digest: z.string() }).optional(),
  mutation: z.strictObject({ kind: z.string(), invocationId: z.string(), id: z.string(), fence: z.number(), baseRevision: z.number(), deadline: RFC3339NanoSchema, resultKey: z.string() }).optional(),
});
/** Private connection head. */
export type ConnectionRecord = z.infer<typeof ConnectionRecordSchema>;

/** Immutable private credential material. */
export const CredentialEnvelopeSchema = z.strictObject({
  schemaVersion: z.literal('connectors.dex.dev/project-connection/v1alpha2'), scope: ScopeSchema,
  connection: z.strictObject({ connectorId: z.string(), connectionName: z.string() }),
  fence: z.number().int().positive(), attemptId: z.string().optional(), credentials: z.record(z.string(), z.unknown()),
  expiresAt: RFC3339NanoSchema.nullable().optional(), authMethod: z.string(),
});
/** Immutable private credential envelope. */
export type CredentialEnvelope = z.infer<typeof CredentialEnvelopeSchema>;
