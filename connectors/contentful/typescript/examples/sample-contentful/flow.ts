// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetEntryStep, newListEntriesStep, type Connection, type GetEntryInput, type GetEntryOutput, type ListEntriesInput, type ListEntriesOutput } from '../../src/index.js';

/** Static Dex Web connection used by both sample Flows. */
export const ConnectionName = 'contentful';

const entryRequest = new Attribute<GetEntryInput>('contentful-entry-request', jsonCodec<GetEntryInput>());
const entryResult = new Attribute<GetEntryOutput>('contentful-entry-result', jsonCodec<GetEntryOutput>());
const listRequest = new Attribute<ListEntriesInput>('contentful-list-request', jsonCodec<ListEntriesInput>());
const listResult = new Attribute<ListEntriesOutput>('contentful-list-result', jsonCodec<ListEntriesOutput>());

/** Read one published entry through the named Contentful connection. */
export class GetEntryFlow implements Flow<GetEntryInput> {
  private readonly prepare = new PrepareGetEntry();
  private readonly complete = new CompleteGetEntry();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Contentful connection name differs'); }
  /** Stable Flow type. */ getFlowType(): string { return 'ContentfulGetEntry'; }
  /** Register start, provider, and terminal Steps. */
  getSteps(): StepList<GetEntryInput> {
    return StepList.startStep(this.prepare).otherSteps(newGetEntryStep({
      stepType: 'GetContentfulEntry',
      annotations: { groupId: 'contentful', groupLabel: 'Contentful', explanation: 'Read one published entry from Contentful.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    }), this.complete);
  }
  /** Register persisted request and result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [entryRequest, entryResult] }; }
}

// dex:group group-id:contentful group-label:"Contentful"
// dex:explanation text:"Persist the requested Contentful entry identity before the provider call."
class PrepareGetEntry implements Step<GetEntryInput> {
  getStepType(): string { return 'PrepareContentfulGetEntry'; }
  execute(context: Context, input: GetEntryInput): StepDecision { entryRequest.set(context, input); return goTo(new StepRef('GetContentfulEntry'), input); }
}

// dex:group group-id:contentful group-label:"Contentful"
// dex:explanation text:"Persist the published entry and complete the Flow."
class CompleteGetEntry implements Step<QueryResult<GetEntryOutput>> {
  getStepType(): string { return 'CompleteContentfulGetEntry'; }
  execute(context: Context, result: QueryResult<GetEntryOutput>): StepDecision { entryResult.set(context, result.value); return gracefulComplete(result.value); }
}

/** Read one bounded published entry page through the named connection. */
export class ListEntriesFlow implements Flow<ListEntriesInput> {
  private readonly prepare = new PrepareListEntries();
  private readonly complete = new CompleteListEntries();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Contentful connection name differs'); }
  /** Stable Flow type. */ getFlowType(): string { return 'ContentfulListEntries'; }
  /** Register start, provider, and terminal Steps. */
  getSteps(): StepList<ListEntriesInput> {
    return StepList.startStep(this.prepare).otherSteps(newListEntriesStep({
      stepType: 'ListContentfulEntries',
      annotations: { groupId: 'contentful', groupLabel: 'Contentful', explanation: 'List one page of published Contentful entries.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    }), this.complete);
  }
  /** Register persisted request and result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [listRequest, listResult] }; }
}

// dex:group group-id:contentful group-label:"Contentful"
// dex:explanation text:"Persist the Contentful page selection before listing entries."
class PrepareListEntries implements Step<ListEntriesInput> {
  getStepType(): string { return 'PrepareContentfulListEntries'; }
  execute(context: Context, input: ListEntriesInput): StepDecision { listRequest.set(context, input); return goTo(new StepRef('ListContentfulEntries'), input); }
}

// dex:group group-id:contentful group-label:"Contentful"
// dex:explanation text:"Persist the published entry page and complete the Flow."
class CompleteListEntries implements Step<QueryResult<ListEntriesOutput>> {
  getStepType(): string { return 'CompleteContentfulListEntries'; }
  execute(context: Context, result: QueryResult<ListEntriesOutput>): StepDecision { listResult.set(context, result.value); return gracefulComplete(result.value); }
}
