// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { branchCase, newQuery, retryCase, type Case, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import type { Failure } from '@superdurable/connectors-sdk';
import { ArticlePageSchema, ConnectorID, newConnectionWithOperations, SearchArticlesBranchProviderRejected, SearchArticlesBranchSearched, SearchArticlesDefinition, type ArticlePage, type Connection, type SearchArticlesInput } from './index.js';
import * as generatedMock from './generated/mock.js';
import type { ArticlePage as GeneratedArticlePage } from './generated/models.js';

function publicBranch(scripted: Case<GeneratedArticlePage>, value: ArticlePage): Case<ArticlePage> {
  if (scripted.kind !== 'branch') throw new TypeError('NewsAPI branch factory returned another case kind');
  return branchCase(scripted.branch, value, scripted.failure);
}

/** Default mock answer from connector.yaml. */
export function searchArticlesSearchedDefaultAnswer(): Case<ArticlePage> {
  const scripted = generatedMock.searchArticlesSearchedDefaultAnswer();
  if (scripted.kind !== 'branch') throw new TypeError('NewsAPI default scenario must select a branch');
  return branchCase(SearchArticlesBranchSearched, ArticlePageSchema.parse(scripted.value), scripted.failure);
}

/** Invalid-key mock answer from connector.yaml. */
export function searchArticlesInvalidKey(): Case<ArticlePage> {
  const scripted = generatedMock.searchArticlesInvalidKey();
  if (scripted.kind !== 'branch') throw new TypeError('NewsAPI invalid-key scenario must select a branch');
  return branchCase(SearchArticlesBranchProviderRejected, { totalResults: 0, articles: [], page: 0, hasMore: false }, scripted.failure);
}

/** Build a searched branch mock case. */
export function searchArticlesSearched(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return publicBranch(generatedMock.searchArticlesSearched(value, failure), value); }
/** Build a provider rejection mock case. */
export function searchArticlesProviderRejected(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return publicBranch(generatedMock.searchArticlesProviderRejected(value, failure), value); }
/** Build an invalid response mock case. */
export function searchArticlesInvalidResponse(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return publicBranch(generatedMock.searchArticlesInvalidResponse(value, failure), value); }
/** Build a local defect mock case. */
export function searchArticlesDefect(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return publicBranch(generatedMock.searchArticlesDefect(value, failure), value); }
/** Build a retry mock case. */
export function searchArticlesRetry(failure: Failure, retryAfterMs = 0): Case<ArticlePage> {
  const scripted = generatedMock.searchArticlesRetry(failure, retryAfterMs);
  if (scripted.kind !== 'retry') throw new TypeError('NewsAPI retry factory returned another case kind');
  return retryCase(scripted.failure, scripted.retryAfterMs);
}

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
