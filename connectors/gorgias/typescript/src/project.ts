// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { GorgiasClient, type GorgiasClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/**
 * Open one static Gorgias connection declared in a Dex project. options, such as a ticketChanged logger or a
 * local apiBaseURL for verification, apply to the connection's client.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: GorgiasClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'gorgias' && entry.connectionName === name);
    if (!selected) throw new TypeError('Gorgias project connection is not configured');
    const raw = z.object({ domain: z.string(), maxResponseBytes: z.number().int().optional(), pollInterval: z.unknown().optional() }).parse(selected.configuration);
    const pollIntervalMs = raw.pollInterval === undefined || raw.pollInterval === null ? undefined : durationMilliseconds(raw.pollInterval);
    const config: Config = {
      domain: raw.domain,
      ...(raw.maxResponseBytes !== undefined ? { maxResponseBytes: raw.maxResponseBytes } : {}),
      ...(pollIntervalMs !== undefined ? { pollIntervalMs } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('gorgias', name, signal);
        const decoded = z.object({ email: z.string().min(1), api_key: z.string().min(1) }).parse(fields);
        return { email: decoded.email, apiKey: new SecretString(decoded.api_key) };
      },
    };
    return new GorgiasClient(config, credentials, options);
  });
}

// The project stores pollInterval as a Go time.Duration: integer nanoseconds, as the Go and Python connectors
// decode it. Zero keeps the one-minute default, as Go does; any other type is rejected.
function durationMilliseconds(value: unknown): number | undefined {
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Gorgias configuration pollInterval has an invalid type');
  return value === 0 ? undefined : Math.ceil(value / 1_000_000);
}
