// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Article, SearchArticlesInput } from '../../src/index.js';

/** Static Dex Web connection name for this Flow. */
export const ConnectionName = 'newsapi';

/** Typed company-news Flow start input. */
export interface CompanyNewsRequest {
  /** Company name to match in article titles. */ company: string;
  /** Days back to search, 1 through 30; zero selects 10. */ days?: number;
  /** Page size, 1 through 100; zero selects 20. */ maxArticles?: number;
  /** NewsAPI language code; empty selects en. */ language?: string;
  /** UTC oldest date fixed by the first Step. */ from?: string;
}

/** Terminal company-news Flow result. */
export interface CompanyNewsOutcome {
  /** Stable successful route. */ status: 'found';
  /** Requested company. */ company: string;
  /** Fixed oldest search date. */ from: string;
  /** NewsAPI match count. */ totalResults: number;
  /** Returned articles. */ articles: Article[];
}

/** Validate a start request and fix the search window before provider retries. */
export function prepareCompanyNewsRequest(request: CompanyNewsRequest, now: Date): Required<CompanyNewsRequest> {
  const company = request.company.trim();
  if (company.length === 0 || [...company].length > 100) throw new TypeError('company must be 1 to 100 characters');
  const days = request.days || 10;
  if (!Number.isSafeInteger(days) || days < 1 || days > 30) throw new TypeError('days must be from 1 through 30');
  const maxArticles = request.maxArticles || 20;
  if (!Number.isSafeInteger(maxArticles) || maxArticles < 1 || maxArticles > 100) throw new TypeError('maxArticles must be from 1 through 100');
  const fromDate = new Date(now);
  fromDate.setUTCDate(fromDate.getUTCDate() - days);
  return { company, days, maxArticles, language: request.language || 'en', from: fromDate.toISOString().slice(0, 10) };
}

/** Map the persisted request to one newest-first NewsAPI title search. */
export function mapToSearchArticlesInput(request: Required<CompanyNewsRequest>): SearchArticlesInput {
  return { query: request.company, searchIn: ['title'], from: request.from, language: request.language, sortBy: 'publishedAt', pageSize: request.maxArticles };
}
