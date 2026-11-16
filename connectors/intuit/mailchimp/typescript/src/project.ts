// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { MailchimpClient, type MailchimpClientOptions } from './client.js';
import { newProjectConnection as newGeneratedProjectConnection } from './generated/connection.js';
import type { Config, Credentials } from './generated/models.js';

/**
 * Open one static Mailchimp connection declared in a Dex project, with its maxResponseBytes and pollInterval.
 * options replaces the transport, API base URL, or clock, for local stand-ins and tests.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: MailchimpClientOptions = {}) {
  return newGeneratedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'mailchimp' && entry.connectionName === name);
    if (!selected) throw new TypeError('Mailchimp project connection is not configured');
    const stored = z.object({ maxResponseBytes: z.number().int().optional(), pollInterval: z.number().int().nullable().optional() }).parse(selected.configuration);
    // Dex Web saves pollInterval as Go decodes a time.Duration: integer nanoseconds.
    const config: Config = { ...(stored.maxResponseBytes !== undefined ? { maxResponseBytes: stored.maxResponseBytes } : {}),
      ...(typeof stored.pollInterval === 'number' ? { pollIntervalMs: stored.pollInterval / 1_000_000 } : {}) };
    const credentials: CredentialProvider<Credentials> = {
      async resolve(call: Call, signal?: AbortSignal): Promise<Credentials> {
        void call;
        const fields = await loadedProject.resolveCredential('mailchimp', name, signal);
        const decoded = z.object({ api_key: z.string().min(1) }).parse(fields);
        return { apiKey: new SecretString(decoded.api_key) };
      },
    };
    return new MailchimpClient(config, credentials, options);
  });
}
