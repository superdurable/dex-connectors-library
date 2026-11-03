// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { S3Client, type S3ClientOptions } from './client.js';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';
import { newConnection as bindConnection } from './generated/connection.js';

export { Connection, newConnection, newConnectionWithOperations, ConnectionNameProvider } from './generated/connection.js';

/** Bind one project connection and resolve current S3 secrets only during Invoke. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: S3ClientOptions = {}) {
  if (!project) throw new TypeError('loaded project is required');
  const selected = project.configuration.connections.find(entry => entry.connectorId === 'amazon-s3' && entry.connectionName === connectionName);
  if (!selected || selected.provider !== 'amazon-s3') throw new TypeError('S3 project connection is not configured');
  const settings = ConfigSchema.parse(selected.configuration);
  const config = Object.fromEntries(Object.entries(settings).filter(([, value]) => value !== undefined)) as Config;
  const credentials: CredentialProvider<Credentials> = {
    async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
      const fields = await project.resolveCredential('amazon-s3', connectionName, signal);
      const decoded = z.object({ access_key_id: z.string(), secret_access_key: z.string(), session_token: z.string().optional() }).parse(fields);
      return { accessKeyId: new SecretString(decoded.access_key_id), secretAccessKey: new SecretString(decoded.secret_access_key), ...(decoded.session_token ? { sessionToken: new SecretString(decoded.session_token) } : {}) };
    },
  };
  return bindConnection(new S3Client(config, credentials, options), { provider: 'amazon-s3', name: connectionName });
}
