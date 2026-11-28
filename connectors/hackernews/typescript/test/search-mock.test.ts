// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { expect, it } from 'vitest';
import { parse } from 'yaml';
import type { Call } from '@superdurable/connectors-sdk';
import { GeneratedMock, searchItemsPublicItems, searchStoriesCompilerStories } from '../src/hackernewsmock.js';

const manifest = parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as {
  spec: { operations: { name: string; mocks: { pages: unknown[] }[] }[] }
};

it.each(['searchItems', 'searchStories'] as const)('%s resolves and records manifest pages, including out-of-range requests', name => {
  const mock = new GeneratedMock('public');
  const query = mock.connection().operations[name]();
  const call = () => ({ id: randomUUID(), connection: mock.connection().reference, operation: query.definition().operation, context: { flowId: 'mock-search' } } as Call);
  const pages = manifest.spec.operations.find(operation => operation.name === name)!.mocks[0]!.pages;
  for (const index of [2, 0, 1]) {
    expect(query.invoke(call(), { page: index })).toMatchObject({ branch: 'searched', value: pages[index] });
  }
  mock.assertNoFailures();
  expect(query.invoke(call(), { page: 7 })).toMatchObject({ branch: 'defect', failure: {kind: 'LOCAL_DEFECT'} });
  expect(mock[name]().calls().at(-1)?.branch).toBe('defect');
  expect(() => mock.assertNoFailures()).toThrow('outside');
});

it('honors explicit scripts before paginated defaults', () => {
  const mock = new GeneratedMock('public');
  const call = { id: randomUUID(), connection: mock.connection().reference, operation: mock.searchItems().definition().operation, context: { flowId: 'mock-search' } } as Call;
  mock.searchItems().respond(searchItemsPublicItems(2));
  expect(mock.searchItems().invoke(call, {page: 15})).toMatchObject({branch: 'searched', value: {page: 2}});
  expect(mock.searchItems().invoke(call, {})).toMatchObject({branch: 'searched', value: {page: 0}});
  mock.searchStories().default(searchStoriesCompilerStories(1));
  expect(mock.searchStories().invoke({...call, operation: mock.searchStories().definition().operation}, {page: 7})).toMatchObject({branch: 'searched', value: {page: 1}});
  mock.assertNoFailures();
});
