// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListInboxesStep, type Connection, type ListInboxesInput, type ListInboxesOutput } from '../../src/index.js';

/** Static Dex Web connection name. */
export const ConnectionName = 'hiver-support';
/** The requested page, retained across retries. */
export const inboxRequest = new Attribute<ListInboxesInput>('hiver-inbox-request', jsonCodec<ListInboxesInput>());
/** The provider page, retained for inspection. */
export const inboxPage = new Attribute<ListInboxesOutput>('hiver-inbox-page', jsonCodec<ListInboxesOutput>());

/** List one page of the account's Hiver shared inboxes. */
export class ListHiverInboxesFlow implements Flow<ListInboxesInput> {
  private readonly prepare = new PrepareListInboxes();
  private readonly complete = new CompleteListInboxes();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Hiver inbox Flow requires its static connection');
  }
  /** Stable Flow type. */ getFlowType(): string { return 'HiverListInboxesFlow'; }
  /** Register the start, connector, and completion Steps. */
  getSteps(): StepList<ListInboxesInput> {
    return StepList.startStep(this.prepare).otherSteps(newListInboxesStep({
      stepType: 'ListHiverInboxes',
      annotations: { groupId: 'hiver-inboxes', groupLabel: 'Hiver shared inboxes', explanation: 'List one page of Hiver shared inboxes.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      listed: this.complete,
    }), this.complete);
  }
  /** Register both durable application Attributes. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [inboxRequest, inboxPage] }; }
}

// dex:group group-id:hiver-inboxes group-label:"Hiver shared inboxes"
// dex:explanation text:"Validate the page request and retain it before calling Hiver."
class PrepareListInboxes implements Step<ListInboxesInput> {
  getStepType(): string { return 'PrepareListHiverInboxes'; }
  execute(context: Context, input: ListInboxesInput): StepDecision {
    if (input.pageSize !== undefined && input.pageSize !== 0 && (!Number.isInteger(input.pageSize) || input.pageSize < 10 || input.pageSize > 100)) return forceFail('pageSize must be zero or from 10 to 100');
    inboxRequest.set(context, input);
    return goTo(new StepRef('ListHiverInboxes'), input);
  }
}

// dex:group group-id:hiver-inboxes group-label:"Hiver shared inboxes"
// dex:explanation text:"Persist the shared inbox page and complete the Flow."
class CompleteListInboxes implements Step<QueryResult<ListInboxesOutput>> {
  getStepType(): string { return 'CompleteListHiverInboxes'; }
  execute(context: Context, result: QueryResult<ListInboxesOutput>): StepDecision {
    inboxPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
