// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newSaveObjectStep, newSearchStep, type Connection, type SavedObject, type SaveObjectInput, type SearchInput, type SearchPage } from '../../src/index.js';

/** Static Dex Web connection name used by this example. */
export const ConnectionName = 'algolia';

/** One index search request; query may be blank to match all records. */
export interface IndexSearchRequest { indexName: string; query: string; hitsPerPage?: number }

/** Search an Algolia index through a named Dex connector connection. */
export class IndexSearchFlow implements Flow<IndexSearchRequest> {
  private readonly searchStep;
  private readonly startStep;
  private readonly searched = new SearchCompleted();

  /** Bind the static connection during Worker registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('index search requires the algolia connection');
    this.searchStep = newSearchStep<IndexSearchRequest>({
      stepType: 'SearchAlgoliaIndex',
      annotations: { groupId: 'algolia', groupLabel: 'Algolia', explanation: 'Search the selected Algolia index and return its bounded hit page.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: (request): SearchInput => ({ indexName: request.indexName, query: request.query, ...(request.hitsPerPage !== undefined ? { hitsPerPage: request.hitsPerPage } : {}) }),
      searched: this.searched,
    });
    this.startStep = new StartSearch(this.searchStep);
  }

  /** Stable Flow type. */
  getFlowType(): string { return 'AlgoliaIndexSearch'; }
  /** Register the start, provider, and completion Steps. */
  getSteps() { return StepList.startStep<IndexSearchRequest>(this.startStep).otherSteps(this.searchStep, this.searched); }
  /** This Flow persists no Attributes or Streams. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

// dex:group group-id:index-search group-label:"Index search"
// dex:explanation text:"Pass the stable index request into the Algolia search Step."
class StartSearch implements Step<IndexSearchRequest> {
  readonly inputCodec = jsonCodec<IndexSearchRequest>();
  constructor(private readonly searchStep: Step<IndexSearchRequest>) {}
  getStepType(): string { return 'StartAlgoliaIndexSearch'; }
  execute(_context: Context, request: IndexSearchRequest): StepDecision { return goTo(this.searchStep, request); }
}

// dex:group group-id:index-search group-label:"Index search"
// dex:explanation text:"Complete the Flow with the Algolia hit page."
class SearchCompleted implements Step<QueryResult<SearchPage>> {
  getStepType(): string { return 'CompleteAlgoliaIndexSearch'; }
  execute(_context: Context, result: QueryResult<SearchPage>): StepDecision { return gracefulComplete(result.value); }
}

/** Replace one record with absolute attributes and return the queued task. */
export class ReplaceObjectFlow implements Flow<SaveObjectInput> {
  private readonly replaceStep;
  private readonly startStep;
  private readonly saved = new ReplacementQueued();

  /** Bind the same static Algolia connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('record replacement requires the algolia connection');
    this.replaceStep = newSaveObjectStep<SaveObjectInput>({
      stepType: 'ReplaceAlgoliaObject',
      annotations: { groupId: 'algolia', groupLabel: 'Algolia', explanation: 'Replace the complete Algolia record at its stable object ID.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: request => request,
      saved: this.saved,
    });
    this.startStep = new StartReplacement(this.replaceStep);
  }

  /** Stable Flow type. */
  getFlowType(): string { return 'AlgoliaReplaceObject'; }
  /** Register replacement Steps. */
  getSteps() { return StepList.startStep<SaveObjectInput>(this.startStep).otherSteps(this.replaceStep, this.saved); }
  /** This Flow persists no Attributes or Streams. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

// dex:group group-id:object-replacement group-label:"Object replacement"
// dex:explanation text:"Pass a stable object ID and complete replacement attributes to Algolia."
class StartReplacement implements Step<SaveObjectInput> {
  readonly inputCodec = jsonCodec<SaveObjectInput>();
  constructor(private readonly replaceStep: Step<SaveObjectInput>) {}
  getStepType(): string { return 'StartAlgoliaReplacement'; }
  execute(_context: Context, request: SaveObjectInput): StepDecision { return goTo(this.replaceStep, request); }
}

// dex:group group-id:object-replacement group-label:"Object replacement"
// dex:explanation text:"Complete the Flow with Algolia's queued indexing task."
class ReplacementQueued implements Step<MutationResult<SavedObject>> {
  getStepType(): string { return 'CompleteAlgoliaReplacement'; }
  execute(_context: Context, result: MutationResult<SavedObject>): StepDecision { return gracefulComplete(result.value); }
}
