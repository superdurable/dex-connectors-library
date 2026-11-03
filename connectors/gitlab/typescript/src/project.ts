// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { GitLabClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/** Bind a named GitLab project connection to the production client. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'gitlab' && entry.connectionName === name);
    if (!selected) throw new TypeError('GitLab project connection is not configured');
    const config = z.object({ baseUrl: z.string().optional(), timeoutMs: z.number().optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('gitlab', name, signal);
        const decoded = z.object({ access_token: z.string().min(1) }).parse(fields);
        return { accessToken: new SecretString(decoded.access_token) };
      },
    };
    return new GitLabClient(config, credentials);
  });
}
