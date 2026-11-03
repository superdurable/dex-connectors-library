// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newEmbedStep, newRerankStep, type Connection, type EmbedInput, type EmbedOutput, type RankedDocument, type RerankInput, type RerankOutput } from '../../src/index.js';

/** Static Dex Web connection name for the Cohere search example. */
export const ConnectionName = 'cohere';

/** Start request for embedding and reranking. */
export interface SearchRequest { query: string; documents: string[] }
/** Ranked result returned on successful completion. */
export interface SearchOutcome { results: RankedDocument[] }

const requestAttribute = new Attribute('cohere-search-request-ts', jsonCodec<SearchRequest>());
const requestCodec = jsonCodec<SearchRequest>();

/** Example Flow that embeds a search query and reranks its candidate documents. */
export class CohereSearchFlow implements Flow<SearchRequest> {
  private readonly complete = new CompleteSearch();
  private readonly rerank;
  private readonly afterEmbed;
  private readonly embed;
  private readonly start;

  /** Bind the static Cohere connection during registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Cohere search requires its static connection');
    this.rerank = newRerankStep<SearchRequest>({
      stepType: 'RerankSearchDocuments',
      annotations: { groupId: 'cohere-search', groupLabel: 'Cohere search', explanation: 'Rank the candidate documents.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: mapToRerankInput,
      ranked: this.complete,
    });
    this.afterEmbed = new AfterEmbedding(this.rerank);
    this.embed = newEmbedStep<SearchRequest>({
      stepType: 'EmbedSearchQuery',
      annotations: { groupId: 'cohere-search', groupLabel: 'Cohere search', explanation: 'Embed the search query.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: mapToEmbedInput,
      embedded: this.afterEmbed,
    });
    this.start = new StartSearch(this.embed);
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'CohereSearchTS'; }
  /** Register all Steps, including both provider calls. */
  getSteps(): StepList<SearchRequest> { return StepList.startStep<SearchRequest>(this.start).otherSteps(this.embed, this.afterEmbed, this.rerank, this.complete); }
  /** Register the request Attribute consumed after embedding. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [requestAttribute] }; }
}

/** Choose the Cohere Embed model for a search query. */
export function mapToEmbedInput(request: SearchRequest): EmbedInput {
  return { model: 'embed-v4.0', texts: [request.query], input_type: 'search_query' };
}

/** Choose the Cohere Rerank model for candidate passages. */
export function mapToRerankInput(request: SearchRequest): RerankInput {
  return { model: 'rerank-v5.0-pro', query: request.query, documents: request.documents };
}

// dex:group group-id:cohere-search group-label:"Cohere search"
// dex:explanation text:"Validate and persist the search request."
class StartSearch implements Step<SearchRequest> {
  readonly inputCodec = requestCodec;
  constructor(private readonly embed: Step<SearchRequest>) {}
  getStepType(): string { return 'StartCohereSearchTS'; }
  execute(context: Context, request: SearchRequest): StepDecision {
    if (typeof request.query !== 'string' || !request.query.trim() || !Array.isArray(request.documents) || request.documents.length < 2) return forceFail('query and at least two documents are required');
    context.setAttribute(requestAttribute, request);
    return goTo(this.embed, request);
  }
}

// dex:group group-id:cohere-search group-label:"Cohere search"
// dex:explanation text:"Read the persisted request after embedding."
class AfterEmbedding implements Step<QueryResult<EmbedOutput>> {
  constructor(private readonly rerank: Step<SearchRequest>) {}
  getStepType(): string { return 'AfterCohereEmbeddingTS'; }
  execute(context: Context, _result: QueryResult<EmbedOutput>): StepDecision {
    void _result;
    const request = context.getAttribute(requestAttribute);
    return goTo(this.rerank, request);
  }
}

// dex:group group-id:cohere-search group-label:"Cohere search"
// dex:explanation text:"Complete with ranked document indices."
class CompleteSearch implements Step<QueryResult<RerankOutput>> {
  getStepType(): string { return 'CompleteCohereSearchTS'; }
  execute(_context: Context, result: QueryResult<RerankOutput>): StepDecision {
    return gracefulComplete({ results: result.value.results ?? [] } satisfies SearchOutcome);
  }
}
