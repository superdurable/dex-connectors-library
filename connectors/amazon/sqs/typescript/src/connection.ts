// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { SQSClient, type SQSClientOptions } from './client.js';
import { newConnection, newConnectionWithOperations, type Connection, type Operations } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export type { Operations } from './generated/connection.js';

/** Bind SQS operations and per-call credentials to a static project connection. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: SQSClientOptions = {}): Connection {
  if (!project) throw new TypeError('SQS connection requires a loaded project');
  const selected = project.configuration.connections.find(entry => entry.connectorId === 'amazon-sqs' && entry.connectionName === connectionName);
  if (!selected || selected.provider !== 'amazon-sqs') throw new TypeError('SQS project connection is not configured');
  const config = z.object({ region: z.string().optional(), endpoint: z.string().optional(), defaultQueueUrl: z.string().optional(),
    maxMessagesPerReceive: z.number().int().optional(), receiveWaitSeconds: z.number().int().optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration) as Config;
  const credentials: CredentialProvider<Credentials> = { async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
    const fields = await project.resolveCredential('amazon-sqs', connectionName, signal);
    const values = z.object({ access_key_id: z.string(), secret_access_key: z.string(), session_token: z.string().optional() }).parse(fields);
    return { accessKeyId: new SecretString(values.access_key_id), secretAccessKey: new SecretString(values.secret_access_key),
      ...(values.session_token ? { sessionToken: new SecretString(values.session_token) } : {}) };
  } };
  return newConnection(new SQSClient(config, credentials, options), { provider: 'amazon-sqs', name: connectionName });
}

/** Bind scripted SQS operations to one static connection name. */
export function newSQSConnectionWithOperations(operations: Operations, connectionName: string): Connection {
  return newConnectionWithOperations(operations, { provider: 'amazon-sqs', name: connectionName });
}
