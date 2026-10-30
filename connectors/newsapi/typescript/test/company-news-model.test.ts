// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { mapToSearchArticlesInput, prepareCompanyNewsRequest } from '../examples/company-news/model.js';

describe('company news Flow input', () => {
  it('fixes the publication window and maps one title search', () => {
    const prepared = prepareCompanyNewsRequest({ company: ' Meridian ' }, new Date('2026-10-30T12:00:00Z'));
    expect(prepared).toEqual({ company: 'Meridian', days: 10, maxArticles: 20, language: 'en', from: '2026-10-20' });
    expect(mapToSearchArticlesInput(prepared)).toEqual({ query: 'Meridian', searchIn: ['title'], from: '2026-10-20', language: 'en', sortBy: 'publishedAt', pageSize: 20 });
  });

  it.each([{ company: '' }, { company: 'x'.repeat(101) }, { company: 'Meridian', days: 31 }, { company: 'Meridian', maxArticles: 101 }])('rejects invalid start input', input => {
    expect(() => prepareCompanyNewsRequest(input, new Date('2026-10-30T12:00:00Z'))).toThrow();
  });
});
