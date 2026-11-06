// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListItemsStep, type Connection, type ListItemsInput, type ListItemsOutput } from '../../src/index.js';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'monday-board';
/** Safe requested board and page selection. */
export const listRequest = new Attribute<ListItemsInput>('monday-list-request', jsonCodec<ListItemsInput>());
/** One persisted result page. */
export const itemPage = new Attribute<ListItemsOutput>('monday-item-page', jsonCodec<ListItemsOutput>());

/** List one bounded page of monday.com board items. */
export class ListItemsFlow implements Flow<ListItemsInput> {
  private readonly prepare = new PrepareListItems();
  private readonly complete = new CompleteListItems();
  private readonly list;

  /** Bind the static monday.com connection at Worker registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('list-items requires its static monday.com connection');
    this.list = newListItemsStep<ListItemsInput>({
      stepType: 'ListMondayItems',
      annotations: { groupId: 'monday-list', groupLabel: 'monday.com Items', explanation: 'Read one bounded page of board items.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'MondayListItemsFlow'; }
  /** Register start, provider, and completion Steps. */ getSteps(): StepList<ListItemsInput> { return StepList.startStep(this.prepare).otherSteps(this.list, this.complete); }
  /** Register both persisted Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [listRequest, itemPage] }; }
}

// dex:group group-id:monday-list group-label:"monday.com Items"
// dex:explanation text:"Validate the board ID and persist the page request before calling monday.com."
class PrepareListItems implements Step<ListItemsInput> {
  getStepType(): string { return 'PrepareMondayListItems'; }
  execute(context: Context, input: ListItemsInput): StepDecision {
    if (!/^[1-9][0-9]{0,19}$/.test(input.boardId?.trim() ?? '')) return forceFail('boardId must be a numeric monday.com board ID');
    listRequest.set(context, input);
    return goTo(new StepRef('ListMondayItems'), input);
  }
}

// dex:group group-id:monday-list group-label:"monday.com Items"
// dex:explanation text:"Persist the listed board page and complete the Flow."
class CompleteListItems implements Step<QueryResult<ListItemsOutput>> {
  getStepType(): string { return 'CompleteMondayListItems'; }
  execute(context: Context, result: QueryResult<ListItemsOutput>): StepDecision { itemPage.set(context, result.value); return gracefulComplete(result.value); }
}
