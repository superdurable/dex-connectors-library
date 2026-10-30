// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile } from 'node:fs/promises';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { defaultConnectorStepExecuteRetryPolicy } from '@superdurable/connectors-sdk';
import { ArticlePageSchema, SearchArticlesDefinition, SearchArticlesMetadata } from '../src/index.js';
import { searchArticlesInvalidKey, searchArticlesSearchedDefaultAnswer } from '../src/newsapimock.js';

interface ManifestMock {
  name: string;
  branch: string;
  default?: boolean;
  output?: unknown;
  failure?: { kind: string; message: string };
}

interface ManifestOperation {
  name: string;
  kind: string;
  idempotency: string;
  authorization: string;
  branches: { id: string; description: string; optional?: boolean }[];
  execution: { executeMethodTimeout: string; durability: string; retry?: unknown };
  mocks: ManifestMock[];
}

describe('NewsAPI manifest and TypeScript contract', () => {
  it('matches every operation route, execution default, and mock case', async () => {
    const manifest = YAML.parse(await readFile(new URL('../../connector.yaml', import.meta.url), 'utf8')) as {
      metadata: { name: string };
      spec: { provider: string; languages: { typescript: { version: string } }; operations: ManifestOperation[] };
    };
    expect(manifest.metadata.name).toBe('newsapi');
    expect(manifest.spec.provider).toBe('newsapi');
    expect(manifest.spec.languages.typescript.version).toBe('v0.1.0');
    expect(manifest.spec.operations).toHaveLength(1);

    const operation = manifest.spec.operations[0];
    expect(operation).toBeDefined();
    if (!operation) throw new Error('NewsAPI operation is missing');
    expect(operation.name).toBe(SearchArticlesDefinition.operation.operationId);
    expect({ kind: operation.kind, idempotency: operation.idempotency, authorization: operation.authorization }).toEqual(SearchArticlesMetadata);
    expect(operation.branches.map(branch => ({ id: branch.id, description: branch.description, ...(branch.optional ? { optional: true } : {}) }))).toEqual(SearchArticlesDefinition.branches);
    expect(operation.execution).toEqual({ executeMethodTimeout: '30s', durability: 'async' });
    expect(SearchArticlesDefinition.stepDefaults).toEqual({ executeMethodTimeoutMs: 30_000, executeDurability: 'async', executeRetry: defaultConnectorStepExecuteRetryPolicy() });

    expect(operation.mocks.map(mock => mock.name)).toEqual(['searchedDefaultAnswer', 'invalidKey']);
    const searched = operation.mocks[0];
    const invalidKey = operation.mocks[1];
    expect(searched?.default).toBe(true);
    const searchedCase = searchArticlesSearchedDefaultAnswer();
    expect(searchedCase.kind).toBe('branch');
    if (searchedCase.kind === 'branch') {
      expect(searchedCase.branch).toBe(searched?.branch);
      expect(searchedCase.value).toEqual(ArticlePageSchema.parse(searched?.output));
    }
    const invalidKeyCase = searchArticlesInvalidKey();
    expect(invalidKeyCase.kind).toBe('branch');
    if (invalidKeyCase.kind === 'branch') {
      expect(invalidKeyCase.branch).toBe(invalidKey?.branch);
      expect(invalidKeyCase.failure).toMatchObject(invalidKey?.failure ?? {});
      expect(invalidKeyCase.failure?.provider).toBe('newsapi');
      expect(invalidKeyCase.failure?.operation).toBe('searchArticles');
    }
  });
});
