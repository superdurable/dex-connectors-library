// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { HackerNewsClient } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import type { Config } from './generated/models.js';

export { HackerNewsClient, GetItemOperation, ListStoryIDsOperation } from './client.js';
export type { HackerNewsClientOptions } from './client.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newGetItemStep, newListStoryIDsStep } from './generated/steps.js';
export * from './generated/definitions.js';
export type { Config, Credentials, GetItemInput, Item, ListStoryIDsInput, StoryIDs } from './generated/models.js';
export { GetItemInputSchema, ListStoryIDsInputSchema } from './generated/models.js';

/** Bind the named public connection and honor all project configuration fields. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'hacker-news-daily' && entry.connectionName === name);
    if (!selected) throw new TypeError('Hacker News project connection is not configured');
    const raw = z.strictObject({ baseUrl: z.string().optional(), timeout: z.number().int().refine(Number.isSafeInteger).optional(), maxResponseBytes: z.number().int().refine(Number.isSafeInteger).optional() }).parse(selected.configuration);
    const config: Config = { baseUrl: raw.baseUrl, timeoutMs: raw.timeout === undefined ? undefined : raw.timeout / 1_000_000, maxResponseBytes: raw.maxResponseBytes };
    return new HackerNewsClient(config);
  });
}
