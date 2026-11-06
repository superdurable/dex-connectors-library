// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { Client } from './client.js';
import { newProjectConnection as bindProjectConnection } from './generated/connection.js';
import { ConfigSchema, type Config, type Credentials } from './generated/models.js';

export { Client, GetPaymentOperation, ListPaymentsOperation } from './client.js';
export type { SquareClientOptions, SquareRequestSender } from './client.js';
export * from './generated/definitions.js';
export * from './generated/models.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newGetPaymentStep, newListPaymentsStep } from './generated/steps.js';
export type { GetPaymentStepConfig, ListPaymentsStepConfig } from './generated/steps.js';

/** Bind a Dex Web Square connection, including its request timeout. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return bindProjectConnection(project, connectionName, (loadedProject, name) => {
    const entry = loadedProject.configuration.connections.find(connection => connection.connectorId === 'square' && connection.connectionName === name);
    if (!entry) throw new TypeError('Square project connection is not configured');
    const fields = z.strictObject({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(entry.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const config: Config = ConfigSchema.parse({ baseUrl: fields.baseUrl, timeoutMs, maxResponseBytes: fields.maxResponseBytes });
    const credentials: CredentialProvider<Credentials> = { async resolve(_call, signal) {
      const privateFields = await loadedProject.resolveCredential('square', name, signal);
      if (typeof privateFields.access_token !== 'string') throw new TypeError('Square token is unavailable');
      return { accessToken: new SecretString(privateFields.access_token) };
    } };
    return new Client(config, credentials);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value; let total = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Square timeout must be a positive Go duration');
    total += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(total) || total <= 0) throw new TypeError('Square timeout must be positive whole milliseconds');
  return total;
}
