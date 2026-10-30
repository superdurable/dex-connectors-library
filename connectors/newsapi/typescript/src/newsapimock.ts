// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { branchCase, newQuery, retryCase, type Case, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import type { Failure } from '@superdurable/connectors-sdk';
import { ArticlePageSchema, ConnectorID, newConnectionWithOperations, SearchArticlesBranchDefect, SearchArticlesBranchInvalidResponse, SearchArticlesBranchProviderRejected, SearchArticlesBranchSearched, SearchArticlesDefinition, type ArticlePage, type Connection, type SearchArticlesInput } from './index.js';

/** Default mock answer from connector.yaml. */
export function searchArticlesSearchedDefaultAnswer(): Case<ArticlePage> {
  return branchCase(SearchArticlesBranchSearched, {
    totalResults: 2, page: 1, hasMore: false,
    articles: [
      { source: { id: 'meridian-news', name: 'Meridian News' }, author: 'A. Rivera', title: 'Meridian opens a research lab', description: 'The company opens a new lab.', url: 'https://news.example.test/meridian-lab', urlToImage: null, publishedAt: '2026-10-26T12:00:00Z', content: 'Meridian opened its newest research lab.' },
      { source: { id: 'meridian-news', name: 'Meridian News' }, author: null, title: 'Meridian reports quarterly results', description: null, url: 'https://news.example.test/meridian-results', urlToImage: null, publishedAt: '2026-10-25T12:00:00Z', content: null },
    ],
  });
}

/** Invalid-key mock answer from connector.yaml. */
export function searchArticlesInvalidKey(): Case<ArticlePage> {
  return branchCase(SearchArticlesBranchProviderRejected, { totalResults: 0, articles: [], page: 0, hasMore: false }, { kind: 'AUTHENTICATION', provider: ConnectorID, operation: 'searchArticles', message: 'Searching with an invalid NewsAPI key is rejected.' });
}

/** Build a searched branch mock case. */
export function searchArticlesSearched(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return branchCase(SearchArticlesBranchSearched, value, failure); }
/** Build a provider rejection mock case. */
export function searchArticlesProviderRejected(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return branchCase(SearchArticlesBranchProviderRejected, value, failure); }
/** Build an invalid response mock case. */
export function searchArticlesInvalidResponse(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return branchCase(SearchArticlesBranchInvalidResponse, value, failure); }
/** Build a local defect mock case. */
export function searchArticlesDefect(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return branchCase(SearchArticlesBranchDefect, value, failure); }
/** Build a retry mock case. */
export function searchArticlesRetry(failure: Failure, retryAfterMs = 0): Case<ArticlePage> { return retryCase(failure, retryAfterMs); }

/** Scripted NewsAPI connection for application tests. */
export class Mock {
  /** Search operation script. */ readonly searchArticles: MockQuery<SearchArticlesInput, ArticlePage>;
  /** Connection consumed by the same Step factory as a real connection. */ readonly connection: Connection;

  /** Create a named connection with the manifest default installed. */
  constructor(connectionName: string) {
    this.searchArticles = newQuery<SearchArticlesInput, ArticlePage>(SearchArticlesDefinition, {
      answerCheck: { branch: SearchArticlesBranchSearched, check: (_input, output) => { ArticlePageSchema.parse(output); } },
    }).default(searchArticlesSearchedDefaultAnswer());
    this.connection = newConnectionWithOperations({ searchArticles: this.searchArticles }, { provider: ConnectorID, name: connectionName });
  }
}
