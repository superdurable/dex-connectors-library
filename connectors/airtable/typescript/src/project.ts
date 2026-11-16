// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { AirtableClient, type AirtableClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/**
 * Open one static Airtable connection declared in a Dex project. options supply the recordChanged Trigger
 * logger and test dependencies.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: AirtableClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'airtable' && entry.connectionName === name);
    if (!selected) throw new TypeError('Airtable project connection is not configured');
    // pollInterval is a Go time.Duration, stored as integer nanoseconds.
    const settings = z.object({
      endpoint: z.string().optional(), maxResponseBytes: z.number().int().optional(),
      pollInterval: z.number().int().nonnegative().optional(), pollPageSize: z.number().int().optional(),
    }).parse(selected.configuration);
    const config: Config = {
      ...(settings.endpoint !== undefined ? { endpoint: settings.endpoint } : {}),
      ...(settings.maxResponseBytes !== undefined ? { maxResponseBytes: settings.maxResponseBytes } : {}),
      ...(settings.pollInterval ? { pollIntervalMs: Math.ceil(settings.pollInterval / 1_000_000) } : {}),
      ...(settings.pollPageSize !== undefined ? { pollPageSize: settings.pollPageSize } : {}),
    };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('airtable', name, signal);
        const decoded = z.object({ personal_access_token: z.string().min(1) }).parse(fields);
        return { personalAccessToken: new SecretString(decoded.personal_access_token) };
      },
    };
    return new AirtableClient(config, credentials, options);
  });
}
