// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { readNextCursor } from '../src/go_url.js';
import { decodePageChangeSearch } from '../src/page_change_search.js';

interface NextLinkCase { nextLink: string; cursor: string; isValid: boolean }
const cases = (JSON.parse(readFileSync(new URL('./fixtures/go-next-link-cursors.json', import.meta.url), 'utf8')) as { cases: NextLinkCase[] }).cases;

describe('the search cursor read from _links.next matches Go readNextCursor', () => {
  it.each(cases.map(testCase => [JSON.stringify(testCase.nextLink), testCase] as const))('%s', (_name, testCase) => {
    const body = new TextEncoder().encode(JSON.stringify({ results: [], _links: { next: testCase.nextLink } }));
    if (testCase.isValid) {
      expect(readNextCursor(testCase.nextLink)).toBe(testCase.cursor);
      expect(decodePageChangeSearch(body).searchCursor).toBe(testCase.cursor);
    } else {
      expect(() => readNextCursor(testCase.nextLink)).toThrow();
      expect(() => decodePageChangeSearch(body)).toThrow('Confluence returned an invalid search cursor');
    }
  });
});
