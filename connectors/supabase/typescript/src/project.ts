// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { SupabaseClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open one Supabase connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'supabase' && entry.connectionName === name);
    if (!selected) throw new TypeError('Supabase project connection is not configured');
    const fields = z.object({ baseUrl: z.string(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = { baseUrl: fields.baseUrl, ...(timeoutMs === undefined ? {} : { timeoutMs }), ...(fields.maxResponseBytes === undefined ? {} : { maxResponseBytes: fields.maxResponseBytes }) };
    const credentials: CredentialProvider<Credentials> = { async resolve(_call, signal) {
      const privateFields = await loadedProject.resolveCredential('supabase', name, signal);
      if (typeof privateFields.api_key !== 'string') throw new TypeError('Supabase API key is unavailable');
      return { apiKey: new SecretString(privateFields.api_key) };
    } };
    return new SupabaseClient(config, credentials);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value; let milliseconds = 0;
  const units: Record<string, number> = { ns: .000001, us: .001, 'µs': .001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Supabase timeout must be a Go duration');
    milliseconds += Number(component[1]) * units[component[2]!]!;
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('Supabase timeout must be positive whole milliseconds');
  return milliseconds;
}
