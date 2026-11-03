// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { OpenWeatherMapClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { ConfigSchema, type Credentials } from './generated/models.js';

/** Open a named OpenWeatherMap connection from Dex project configuration. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'openweathermap' && entry.connectionName === name);
    if (!selected) throw new TypeError('OpenWeatherMap project connection is not configured');
    const parsed = ConfigSchema.parse(selected.configuration);
    const config = { ...(parsed.baseUrl !== undefined ? { baseUrl: parsed.baseUrl } : {}), ...(parsed.timeoutMs !== undefined ? { timeoutMs: parsed.timeoutMs } : {}), ...(parsed.maxResponseBytes !== undefined ? { maxResponseBytes: parsed.maxResponseBytes } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('openweathermap', name, signal);
        const decoded = z.object({ api_key: z.string().min(1) }).parse(fields);
        return { apiKey: new SecretString(decoded.api_key) };
      },
    };
    return new OpenWeatherMapClient(config, credentials);
  });
}
