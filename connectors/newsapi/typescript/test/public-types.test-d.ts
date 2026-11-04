// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Query, QueryStep } from '@superdurable/connectors-sdk';
import type { MockQuery } from '@superdurable/connectors-sdk/connectormock';
import { newSearchArticlesStep, type ArticlePage, type Connection, type SearchArticlesInput } from '../src/index.js';
import type { Mock } from '../src/newsapimock.js';

type Exact<Left, Right> = (<Value>() => Value extends Left ? 1 : 2) extends (<Value>() => Value extends Right ? 1 : 2) ?
  ((<Value>() => Value extends Right ? 1 : 2) extends (<Value>() => Value extends Left ? 1 : 2) ? true : false) : false;
type Assert<Value extends true> = Value;
type StepInput = { id: string };

export type PublicTypeAssertions = [
  Assert<Exact<Connection['operations']['searchArticles'], Query<SearchArticlesInput, ArticlePage>>>,
  Assert<Exact<ReturnType<typeof newSearchArticlesStep<StepInput>>, QueryStep<StepInput, SearchArticlesInput, ArticlePage>>>,
  Assert<Exact<Mock['searchArticles'], MockQuery<SearchArticlesInput, ArticlePage>>>,
];
