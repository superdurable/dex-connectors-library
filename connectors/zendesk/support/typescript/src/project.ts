// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { ZendeskClient, type ZendeskClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/**
 * Open one static Zendesk Support connection declared in a Dex project. options, such as a ticketChanged logger, a
 * High Volume export request rate, or a local apiBaseURL for verification, apply to the connection's client.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ZendeskClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'zendesk-support' && entry.connectionName === name);
    if (!selected) throw new TypeError('Zendesk Support project connection is not configured');
    const parsed = z.object({ subdomain: z.string(), maxResponseBytes: z.number().int().optional(), pollInterval: z.union([z.number().int(), z.string()]).optional() }).parse(selected.configuration);
    const config: Config = {
      subdomain: parsed.subdomain,
      ...(parsed.maxResponseBytes !== undefined ? { maxResponseBytes: parsed.maxResponseBytes } : {}),
      ...(parsed.pollInterval !== undefined ? { pollIntervalMs: durationMilliseconds(parsed.pollInterval) } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('zendesk-support', name, signal);
        const decoded = z.object({ email: z.string(), api_token: z.string().min(1) }).parse(fields);
        return { email: decoded.email, apiToken: new SecretString(decoded.api_token) };
      },
    };
    return new ZendeskClient(config, credentials, options);
  });
}

// Dex Web stores a Go duration as integer nanoseconds; a hand-written project may use Go duration text.
// Rounding down keeps Go's 5s minimum: 4.9999999s stays below it.
function durationMilliseconds(value: number | string): number {
  if (typeof value === 'number') return Math.floor(value / 1_000_000);
  let rest = value;
  let total = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, '\u00b5s': 0.001, ms: 1, s: 1000, m: 60000, h: 3600000 };
  while (rest) {
    const match = /^(\d+(?:\.\d+)?)(ns|us|\u00b5s|ms|s|m|h)/.exec(rest);
    if (!match) throw new TypeError('Zendesk pollInterval must use Go duration syntax');
    total += Number(match[1]) * units[match[2]!]!;
    rest = rest.slice(match[0].length);
  }
  if (!Number.isFinite(total) || total <= 0 || total > Number.MAX_SAFE_INTEGER) throw new TypeError('Zendesk pollInterval must be positive');
  return Math.floor(total);
}
