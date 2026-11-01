// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQuery, type Case, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import type { Failure } from '@superdurable/connectors-sdk';
import { ArticlePageSchema, ConnectorID, newConnectionWithOperations, SearchArticlesBranchSearched, SearchArticlesDefinition, type ArticlePage, type Connection, type SearchArticlesInput } from './index.js';
import * as generatedMock from './generated/mock.js';

/** Default mock answer from connector.yaml. */
export function searchArticlesSearchedDefaultAnswer(): Case<ArticlePage> {
  const scripted = generatedMock.searchArticlesSearchedDefaultAnswer();
  if (scripted.kind !== 'branch') throw new TypeError('NewsAPI default scenario must select a branch');
  return { ...scripted, value: ArticlePageSchema.parse(scripted.value) };
}

/** Invalid-key mock answer from connector.yaml. */
export function searchArticlesInvalidKey(): Case<ArticlePage> {
  const scripted = generatedMock.searchArticlesInvalidKey();
  if (scripted.kind !== 'branch') throw new TypeError('NewsAPI invalid-key scenario must select a branch');
  return { ...scripted, value: { ...scripted.value, articles: scripted.value.articles ?? [] } };
}

/** Build a searched branch mock case. */
export function searchArticlesSearched(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return generatedMock.searchArticlesSearched(value, failure) as Case<ArticlePage>; }
/** Build a provider rejection mock case. */
export function searchArticlesProviderRejected(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return generatedMock.searchArticlesProviderRejected(value, failure) as Case<ArticlePage>; }
/** Build an invalid response mock case. */
export function searchArticlesInvalidResponse(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return generatedMock.searchArticlesInvalidResponse(value, failure) as Case<ArticlePage>; }
/** Build a local defect mock case. */
export function searchArticlesDefect(value: ArticlePage, failure?: Failure): Case<ArticlePage> { return generatedMock.searchArticlesDefect(value, failure) as Case<ArticlePage>; }
/** Build a retry mock case. */
export function searchArticlesRetry(failure: Failure, retryAfterMs = 0): Case<ArticlePage> {
  return generatedMock.searchArticlesRetry(failure, retryAfterMs) as Case<ArticlePage>;
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
