// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { WebFeedClient, type WebFeedClientOptions } from './client.js';
import { newProjectConnection as generatedProjectConnection, type Connection } from './generated/connection.js';
import type { Config } from './generated/models.js';

const ProjectSettingsSchema = z.strictObject({
  timeout: z.union([z.string(), z.number().int()]).optional(),
  maxDocumentBytes: z.number().int().optional(),
  maxPageBytes: z.number().int().optional(),
  userAgent: z.string().optional(),
});

/**
 * Open one named web-feed connection from Dex project configuration. The connection has no credentials; its
 * settings are read once here, and blank settings take the manifest defaults.
 */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: WebFeedClientOptions = {}): Connection {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'web-feed' && entry.connectionName === name);
    if (!selected) throw new TypeError('web-feed project connection is not configured');
    const fields = ProjectSettingsSchema.parse(selected.configuration ?? {});
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = {
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(fields.maxDocumentBytes ? { maxDocumentBytes: fields.maxDocumentBytes } : {}),
      ...(fields.maxPageBytes ? { maxPageBytes: fields.maxPageBytes } : {}),
      ...(fields.userAgent ? { userAgent: fields.userAgent } : {}),
    };
    return new WebFeedClient(config, options);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const part = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!part) throw new TypeError('web-feed timeout must be a positive Go duration');
    milliseconds += Number(part[1]) * (units[part[2] ?? ''] ?? Number.NaN);
    remaining = remaining.slice(part[0].length);
  }
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) throw new TypeError('web-feed timeout must resolve to positive whole milliseconds');
  return milliseconds;
}
