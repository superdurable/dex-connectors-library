// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { DiscordClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';

/** Open a static Discord connection from a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'discord' && entry.connectionName === name);
    if (!selected) throw new TypeError('Discord project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('discord', name, signal);
        const decoded = z.object({ bot_token: z.string().min(1) }).parse(fields);
        return { botToken: new SecretString(decoded.bot_token) };
      },
    };
    return new DiscordClient(config, credentials);
  });
}
