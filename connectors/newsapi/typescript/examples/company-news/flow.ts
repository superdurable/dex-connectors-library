// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import { z } from 'zod';
import { newSearchArticlesStep, type ArticlePage, type Connection } from '../../src/index.js';
import { ConnectionName, mapToSearchArticlesInput, prepareCompanyNewsRequest, type CompanyNewsOutcome, type CompanyNewsRequest } from './model.js';
import type { QueryResult } from '@superdurable/connectors-sdk';

const requestSchema = z.object({ company: z.string(), days: z.number().int().optional(), maxArticles: z.number().int().optional(), language: z.string().optional(), from: z.string().optional() });
const requestCodec = jsonCodec<CompanyNewsRequest>({ typeName: 'CompanyNewsRequest', decode: value => requestSchema.parse(value) });
const companyNewsRequestAttribute = new Attribute('newsapi-company-news-request', jsonCodec<Required<CompanyNewsRequest>>());
const companyNewsOutcomeAttribute = new Attribute('newsapi-company-news-outcome', jsonCodec<CompanyNewsOutcome>());

/** NewsAPI company-news Flow using one generated-style operation factory. */
export class CompanyNewsFlow implements Flow<CompanyNewsRequest> {
  private readonly articlesFound = new CompanyArticlesFound();
  private readonly searchCompanyArticles;
  private readonly recordRequest;

  /** Bind a named real or scripted connection during Flow registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('company news requires its static NewsAPI connection');
    this.searchCompanyArticles = newSearchArticlesStep<Required<CompanyNewsRequest>>({
      stepType: 'SearchCompanyArticles',
      annotations: { groupId: 'newsapi', groupLabel: 'NewsAPI', explanation: 'Search NewsAPI for the newest articles whose titles name the company.' },
      connection, connectionName: 'newsapi',
      mapToOperationInput: mapToSearchArticlesInput,
      searched: this.articlesFound,
    });
    this.recordRequest = new RecordCompanyNewsRequest(this.searchCompanyArticles);
  }

  /** Return the stable Dex Flow identity. */ getFlowType(): string { return 'NewsAPICompanyNews'; }
  /** Register the start, provider, and completion Steps once each. */
  getSteps(): StepList<CompanyNewsRequest> { return StepList.startStep<CompanyNewsRequest>(this.recordRequest).otherSteps(this.searchCompanyArticles, this.articlesFound); }
  /** Register both durable application Attributes. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [companyNewsRequestAttribute, companyNewsOutcomeAttribute] }; }
}

// dex:group group-id:company-news group-label:"Company news"
// dex:explanation text:"Validate the company, fix the search window, and persist the request before calling NewsAPI."
class RecordCompanyNewsRequest implements Step<CompanyNewsRequest> {
  readonly inputCodec = requestCodec;
  constructor(private readonly searchCompanyArticles: Step<Required<CompanyNewsRequest>>) {}
  getStepType(): string { return 'RecordCompanyNewsRequest'; }
  execute(context: Context, request: CompanyNewsRequest): StepDecision {
    let prepared: Required<CompanyNewsRequest>;
    try { prepared = prepareCompanyNewsRequest(request, context.flowStartedAt); }
    catch (error) { return forceFail(error instanceof Error ? error.message : 'company news request is invalid'); }
    context.setAttribute(companyNewsRequestAttribute, prepared);
    return goTo(this.searchCompanyArticles, prepared);
  }
}

// dex:group group-id:company-news group-label:"Company news"
// dex:explanation text:"Record the matching articles and complete the Flow."
class CompanyArticlesFound implements Step<QueryResult<ArticlePage>> {
  getStepType(): string { return 'CompanyArticlesFound'; }
  execute(context: Context, result: QueryResult<ArticlePage>): StepDecision {
    const request = context.getAttribute(companyNewsRequestAttribute);
    const outcome: CompanyNewsOutcome = {
      status: 'found', company: request.company, from: request.from,
      totalResults: result.value.totalResults, articles: result.value.articles,
    };
    context.setAttribute(companyNewsOutcomeAttribute, outcome);
    return gracefulComplete(outcome);
  }
}
