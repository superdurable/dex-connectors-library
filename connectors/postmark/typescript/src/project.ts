// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { PostmarkClient } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import { newProjectConnection as bindProjectConnection } from './generated/connection.js';

const projectFields = z.strictObject({
  baseUrl: z.string().optional(),
  timeout: z.union([z.string(), z.number().int()]).optional(),
  maxResponseBytes: z.number().int().optional(),
});

/** Open a Postmark Dex Web connection without putting credentials in Flow state. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return bindProjectConnection(project, connectionName, (loaded, name) => {
    const selected = loaded.configuration.connections.find(entry => entry.connectorId === ConnectorID && entry.connectionName === name);
    if (!selected) throw new TypeError('Postmark project connection is not configured');
    const fields = projectFields.parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    return new PostmarkClient({
      ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}),
      ...(timeoutMs !== undefined ? { timeoutMs } : {}),
      ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
    }, {
      async resolve(_call, signal) {
        const privateFields = await loaded.resolveCredential(ConnectorID, name, signal);
        if (typeof privateFields.api_key !== 'string') throw new TypeError('Postmark API key is unavailable');
        return { apiKey: new SecretString(privateFields.api_key) };
      },
    });
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let result = 0;
  const unitMilliseconds: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Postmark timeout must be a Go duration');
    result += Number(component[1]) * (unitMilliseconds[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(Math.ceil(result)) || result < 0) throw new TypeError('Postmark timeout is invalid');
  return Math.ceil(result);
}
