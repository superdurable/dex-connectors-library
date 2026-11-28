// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { HackerNewsClient } from './client.js';
import { newProjectConnection as generatedProjectConnection } from './generated/connection.js';
import type { Config } from './generated/models.js';

export { HackerNewsClient, GetArticleThreadOperation, GetItemOperation, GetUserOperation, ListStoryIDsOperation, MaxStoryTextCharacters, SearchItemsOperation, SearchStoriesOperation } from './client.js';
export type { HackerNewsClientOptions } from './client.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newGetArticleThreadStep, newGetItemStep, newGetUserStep, newListStoryIDsStep, newSearchItemsStep, newSearchStoriesStep } from './generated/steps.js';
export type { GetArticleThreadStepConfig, GetItemStepConfig, GetUserStepConfig, ListStoryIDsStepConfig, SearchItemsStepConfig, SearchStoriesStepConfig } from './generated/steps.js';
export * from './generated/definitions.js';
export type { ArticleComment, ArticleThread, GetArticleThreadInput, Config, Credentials, GetItemInput, GetUserInput, User, Item, ItemSearchHit, ItemSearchPage, ListStoryIDsInput, SearchItemsInput, SearchStoriesInput, StoryIDs, StorySearchHit, StorySearchPage } from './generated/models.js';
export { ArticleCommentSchema, ArticleThreadSchema, GetArticleThreadInputSchema, GetItemInputSchema, GetUserInputSchema, UserSchema, ItemSearchHitSchema, ItemSearchPageSchema, ListStoryIDsInputSchema, SearchItemsInputSchema, SearchStoriesInputSchema, StorySearchHitSchema, StorySearchPageSchema } from './generated/models.js';

/** Bind the named public connection and honor all project configuration fields. */
export function newProjectConnection(project: LoadedProject, connectionName: string) {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === 'hacker-news-daily' && entry.connectionName === name);
    if (!selected) throw new TypeError('Hacker News project connection is not configured');
    const raw = z.strictObject({ baseUrl: z.string().optional(), timeout: z.number().int().refine(Number.isSafeInteger).optional(), maxResponseBytes: z.number().int().refine(Number.isSafeInteger).optional(), searchBaseUrl: z.string().optional() }).parse(selected.configuration);
    const config: Config = { baseUrl: raw.baseUrl, timeoutMs: raw.timeout === undefined ? undefined : raw.timeout / 1_000_000, maxResponseBytes: raw.maxResponseBytes, searchBaseUrl: raw.searchBaseUrl };
    return new HackerNewsClient(config);
  });
}
