// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionRefSchema } from '@superdurable/connectors-sdk';
import { newRefreshingCredentialProvider, type LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { GmailClient, type GmailClientOptions } from './client.js';
import { GmailConnection } from './connection.js';
import { decodeGmailCredentials, decodeGmailSharedCredentials, encodeGmailCredentials } from './credential_codec.js';
import { ConnectorID, ProviderID, type Config } from './models.js';

const ProjectGmailConfigurationSchema = z.object({
  endpoint: z.string().optional(), maxResponseBytes: z.number().int().safe().optional(),
  maxMessageBytes: z.number().int().safe().optional(), pollInterval: z.number().int().safe().optional(),
  pollPageSize: z.number().int().safe().optional(),
});

/** Open one statically named Gmail connection from a loaded Dex project. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: GmailClientOptions = {}): GmailConnection {
  return new GmailConnection(newProjectGmailClient(project, connectionName, options), { provider: ProviderID, name: connectionName });
}

/** Bind startup-pinned settings to credentials refreshed in private project storage. */
export function newProjectGmailClient(project: LoadedProject, connectionName: string, options: GmailClientOptions = {}): GmailClient {
  if (!project) throw new TypeError('Gmail connection requires the loaded project configuration');
  ConnectionRefSchema.parse({ provider: ProviderID, name: connectionName });
  const entry = project.configuration.connections.find(connection => connection.connectorId === ConnectorID && connection.connectionName === connectionName);
  if (!entry) throw new TypeError('Gmail project connection is not configured');
  if (entry.provider !== ProviderID) throw new TypeError('Gmail project connection provider differs from the manifest');
  const fields = ProjectGmailConfigurationSchema.parse(entry.configuration);
  const config: Config = {
    ...(fields.endpoint ? { endpoint: fields.endpoint } : {}),
    ...(fields.maxResponseBytes ? { maxResponseBytes: fields.maxResponseBytes } : {}),
    ...(fields.maxMessageBytes ? { maxMessageBytes: fields.maxMessageBytes } : {}),
    ...(fields.pollInterval ? { pollIntervalMs: Math.ceil(fields.pollInterval / 1_000_000) } : {}),
    ...(fields.pollPageSize ? { pollPageSize: fields.pollPageSize } : {}),
  };
  const provider = newRefreshingCredentialProvider(project.connections, { connectorId: ConnectorID, connectionName }, decodeGmailCredentials, encodeGmailCredentials, decodeGmailSharedCredentials);
  return new GmailClient(config, provider, options);
}
