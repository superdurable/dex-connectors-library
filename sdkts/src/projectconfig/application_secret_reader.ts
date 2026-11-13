// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { z } from 'zod';
import { isApplicationSecretReferenceInScope, isValidEnvironmentValueEncoding, replaceUnpairedSurrogates } from './application_environment_rules.js';
import { ScopeSchema, scopePrefix, type ApplicationSecretRef, type Scope } from './model.js';
import type { ObjectStore } from './storage.js';

// Internal module, not exported from index.ts: Go keeps readSecretValue private.

export const CredentialBrokerTokenSecretName = 'DEX_CREDENTIAL_BROKER_TOKEN';

// Go's strict decoder leaves a missing or null value as the empty string.
const ApplicationSecretSchema = z.strictObject({ scope: ScopeSchema, name: z.string(), value: z.preprocess(value => value ?? '', z.string()) });

// Mirrors Go's readSecretValue: exact version, complete-object digest, scope, and name. Errors never contain the value.
export async function readApplicationSecretValue(objects: ObjectStore, scope: Scope, reference: ApplicationSecretRef, name: string): Promise<string> {
  if (!isApplicationSecretReferenceInScope(reference, scopePrefix(scope), name)) throw new Error('application secret reference differs from scope');
  const object = await objects.readObject(reference.key, reference.version);
  let decoded: unknown;
  try { decoded = JSON.parse(Buffer.from(object.contents).toString('utf8')) as unknown; }
  catch { decoded = undefined; }
  const secret = ApplicationSecretSchema.safeParse(decoded);
  const value = secret.success ? replaceUnpairedSurrogates(secret.data.value) : '';
  const digest = `sha256:${createHash('sha256').update(object.contents).digest('hex')}`;
  if (object.key !== reference.key || object.version !== reference.version || digest !== reference.digest || !secret.success ||
    secret.data.scope.projectId !== scope.projectId || secret.data.scope.kind !== scope.kind || secret.data.name !== name || !isValidEnvironmentValueEncoding(value)) {
    throw new Error('application secret integrity or scope differs');
  }
  return value;
}
