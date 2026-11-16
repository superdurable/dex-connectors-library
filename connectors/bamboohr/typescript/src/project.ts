// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { BambooHRClient, type BambooHRClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/**
 * Open one static BambooHR connection declared in a Dex project, with its stored companyDomain,
 * maxResponseBytes, and pollInterval. options replaces the transport or clock, for example in tests.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: BambooHRClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'bamboohr' && entry.connectionName === name);
    if (!selected) throw new TypeError('BambooHR project connection is not configured');
    const config = z.object({ companyDomain: z.string(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('bamboohr', name, signal);
        const decoded = z.object({ api_key: z.string().min(1) }).parse(fields);
        return { apiKey: new SecretString(decoded.api_key) };
      },
    };
    const pollIntervalMs = pollIntervalMilliseconds(selected.configuration.pollInterval);
    return new BambooHRClient({ ...config, ...(pollIntervalMs !== undefined ? { pollIntervalMs } : {}) }, credentials, options);
  });
}

/** Read the stored pollInterval as Go decodes a time.Duration: integer nanoseconds, as Dex Web saves it. */
function pollIntervalMilliseconds(value: unknown): number | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('BambooHR configuration pollInterval has an invalid type');
  return value / 1_000_000;
}
