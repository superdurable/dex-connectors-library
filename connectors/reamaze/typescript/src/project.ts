// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { ReamazeClient, type ReamazeClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Open a static Re:amaze connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ReamazeClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'reamaze' && entry.connectionName === name);
    if (!selected) throw new TypeError('Re:amaze project connection is not configured');
    const stored = z.object({ brand: z.string(), maxResponseBytes: z.number().int().optional(), pollInterval: z.number().int().nullable().optional() }).parse(selected.configuration);
    // Dex Web saves pollInterval as Go decodes a time.Duration: integer nanoseconds.
    const config: Config = { brand: stored.brand, ...(stored.maxResponseBytes !== undefined ? { maxResponseBytes: stored.maxResponseBytes } : {}),
      ...(typeof stored.pollInterval === 'number' ? { pollIntervalMs: stored.pollInterval / 1_000_000 } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('reamaze', name, signal);
        const decoded = z.object({ email: z.string(), api_token: z.string().min(1) }).parse(fields);
        return { email: decoded.email, apiToken: new SecretString(decoded.api_token) };
      },
    };
    return new ReamazeClient(config, credentials, options);
  });
}
