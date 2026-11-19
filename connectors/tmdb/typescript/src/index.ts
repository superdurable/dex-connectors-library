// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { TMDBClient } from './client.js';
import { newConnection, type Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import type { Config, Credentials } from './generated/models.js';

export { TMDBClient } from './client.js';
export type { TMDBClientOptions, TMDBRequestSender } from './client.js';
export { SearchMoviesOperation, GetMovieOperation } from './operations.js';
export { newSearchMoviesStep, newGetMovieStep } from './generated/steps.js';
export { newConnection, Connection } from './generated/connection.js';
export * from './generated/definitions.js';
export type { Config, Credentials, SearchMoviesInput, GetMovieInput, Movie, MoviePage } from './generated/models.js';

/** Bind one configured Dex Web connection and resolve its secret for each call. */
export function newProjectConnection(project: LoadedProject, connectionName: string): Connection {
  if (!project || !connectionName.trim()) throw new TypeError('TMDB project connection is required');
  const configured = project.configuration.connections.find(entry => entry.connectorId === ConnectorID && entry.connectionName === connectionName);
  if (!configured || configured.provider !== 'tmdb') throw new TypeError('TMDB project connection is not configured');
  const fields = z.object({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(configured.configuration);
  const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
  const config: Config = {
    ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}),
    ...(timeoutMs !== undefined ? { timeoutMs } : {}),
    ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}),
  };
  const credentials: CredentialProvider<Credentials> = { async resolve(_call, signal) {
    const privateFields = await project.resolveCredential(ConnectorID, connectionName, signal);
    if (typeof privateFields.api_key !== 'string') throw new TypeError('TMDB API key is unavailable');
    return { apiKey: new SecretString(privateFields.api_key) };
  } };
  return newConnection(new TMDBClient(config, credentials), { provider: 'tmdb', name: connectionName });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let milliseconds = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('TMDB timeout must be a positive Go duration');
    milliseconds += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) throw new TypeError('TMDB timeout must be positive');
  return Math.ceil(milliseconds);
}
