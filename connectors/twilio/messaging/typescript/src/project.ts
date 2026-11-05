// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newCredentialProvider, type LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { ConfigSchema, decodeCredentials, type Config } from './generated/models.js';
import { newConnection as generatedNewConnection, type Connection } from './generated/connection.js';
import { TwilioClient, type TwilioClientOptions } from './client.js';

/** Bind a validated Twilio client to a static connection. */
export function newConnection(client: TwilioClient, name: string): Connection {
  return generatedNewConnection(client, { provider: 'twilio', name });
}
/** Build a Twilio client from one project connection and its private credentials. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: TwilioClientOptions = {}): Connection {
  const selected = project.configuration.connections.find(entry => entry.connectorId === 'twilio-messaging' && entry.connectionName === connectionName);
  if (!selected || selected.provider !== 'twilio') throw new TypeError('Twilio project connection is not configured');
  const settings = z.object({ accountSid: z.string(), defaultSender: z.string().optional(), endpoint: z.string().optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
  const config: Config = ConfigSchema.parse(settings);
  const credentialProvider = newCredentialProvider(project.connections, { connectorId: 'twilio-messaging', connectionName }, decodeCredentials);
  return newConnection(new TwilioClient(config, credentialProvider, options), connectionName);
}
