// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { ElevenLabsClient } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';

/** Open one static ElevenLabs connection declared in a Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'elevenlabs' && entry.connectionName === name);
    if (!selected) throw new TypeError('ElevenLabs project connection is not configured');
    const config = ConfigSchema.parse(selected.configuration) as Config;
    const credentials: CredentialProvider<Credentials> = {
      async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
        const fields = await loadedProject.resolveCredential('elevenlabs', name, signal);
        const decoded = z.object({ api_key: z.string().min(1) }).parse(fields);
        return { apiKey: new SecretString(decoded.api_key) };
      },
    };
    return new ElevenLabsClient(config, credentials);
  });
}
