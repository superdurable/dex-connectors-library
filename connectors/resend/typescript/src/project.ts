// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { ResendClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Bind one static Resend connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'resend' && entry.connectionName === name);
    if (!selected) throw new TypeError('Resend project connection is not configured');
    const config = z.object({ baseUrl: z.string().optional(), timeoutMs: z.number().int().optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('resend', name, signal);
        const decoded = z.object({ api_key: z.string().min(1) }).parse(fields);
        return { apiKey: new SecretString(decoded.api_key) };
      },
    };
    return new ResendClient(config, credentials);
  });
}
