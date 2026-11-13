// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ApplicationSecretRef, EnvironmentValue } from './model.js';

// This module is internal: index.ts re-exports only isApplicationEnvironmentName, which Go also exports.

export const MaximumEnvironmentValueBytes = 32768;
export const MaximumEnvironmentFields = 128;

const applicationEnvironmentNamePattern = /^[A-Z][A-Z0-9_]{0,127}$/;
const reservedEnvironmentNamePrefixes = ['SUPERVERSE_', 'DEX_', 'AWS_', 'LD_', 'GO', 'GIT_'];
const reservedEnvironmentNames = new Set(['PATH', 'HOME', 'PORT', 'HOST', 'NODE_OPTIONS', 'SSL_CERT_FILE', 'SSL_CERT_DIR', 'PUBLIC_BASE_URL', 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'NO_PROXY']);
const unpairedSurrogatePattern = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/;
const unpairedSurrogatesPattern = new RegExp(unpairedSurrogatePattern.source, 'g');
const applicationSecretIdentifierPattern = /^[a-f0-9]{32}$/;
const applicationSecretDigestPattern = /^sha256:[a-f0-9]{64}$/;

/**
 * Report whether a name is safe for an application-owned value.
 *
 * Names are uppercase ASCII identifiers of at most 128 characters. Deployment, SDK, AWS, process-loader, toolchain,
 * trust-root, and proxy variables are reserved: the prefixes `SUPERVERSE_`, `DEX_`, `AWS_`, `LD_`, `GO`, and `GIT_`,
 * and the names `PATH`, `HOME`, `PORT`, `HOST`, `NODE_OPTIONS`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `PUBLIC_BASE_URL`,
 * `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, and `NO_PROXY`.
 */
export function isApplicationEnvironmentName(name: string): boolean {
  return typeof name === 'string' && applicationEnvironmentNamePattern.test(name) &&
    !reservedEnvironmentNamePrefixes.some(prefix => name.startsWith(prefix)) && !reservedEnvironmentNames.has(name);
}

// Go's validEnvironmentValue: valid UTF-8, no NUL, at most 32768 bytes.
export function isValidEnvironmentValueEncoding(value: string): boolean {
  return typeof value === 'string' && Buffer.byteLength(value, 'utf8') <= MaximumEnvironmentValueBytes && !value.includes('\0') && !unpairedSurrogatePattern.test(value);
}

// Go's JSON decoder turns an unpaired surrogate escape into U+FFFD; JSON.parse keeps it.
export function replaceUnpairedSurrogates(value: string): string {
  return value.replace(unpairedSurrogatesPattern, '�');
}

export function applicationSecretPrefix(scopePrefix: string, name: string): string {
  return `${scopePrefix}/app-secrets/${name}/`;
}

export function isApplicationSecretReferenceInScope(reference: ApplicationSecretRef, scopePrefix: string, name: string): boolean {
  const prefix = applicationSecretPrefix(scopePrefix, name);
  return reference.key.startsWith(prefix) && applicationSecretIdentifierPattern.test(reference.key.slice(prefix.length)) &&
    reference.version !== '' && reference.version !== 'null' && !/[\0\r\n]/.test(reference.version) && applicationSecretDigestPattern.test(reference.digest);
}

// Mirrors Go's validateEnvironmentReferences; errors never contain values.
export function validateEnvironmentReferences(scopePrefix: string, environment: Readonly<Record<string, EnvironmentValue>>): void {
  const entries = Object.entries(environment);
  if (entries.length > MaximumEnvironmentFields) throw new Error('application environment exceeds field limit');
  for (const [name, entry] of entries) {
    const value = entry?.value ?? undefined;
    const secretRef = entry?.secretRef ?? undefined;
    if (!isApplicationEnvironmentName(name) || (value === undefined) === (secretRef === undefined)) throw new Error('application environment entry is invalid');
    if (value !== undefined) {
      if (!isValidEnvironmentValueEncoding(replaceUnpairedSurrogates(value))) throw new Error('application environment value is invalid');
    } else if (!secretRef || !isApplicationSecretReferenceInScope(secretRef, scopePrefix, name)) throw new Error('application secret reference differs from scope');
  }
}
