// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListCardsStep, type Connection, type ListCardsInput, type ListCardsOutput } from '@superdurable/connector-atlassian-trello';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'trello-approved-board';
/** Public board selection retained for Flow inspection. */
export const cardRequest = new Attribute<ListCardsInput>('trello-card-request', jsonCodec<ListCardsInput>());
/** Safe card page retained for Flow inspection. */
export const cardPage = new Attribute<ListCardsOutput>('trello-card-page', jsonCodec<ListCardsOutput>());

/** One bounded Trello board page through a named private connection. */
export class ListCardsFlow implements Flow<ListCardsInput> {
  private readonly prepare = new PrepareListCards();
  private readonly complete = new CompleteListCards();

  /** Bind the connection before Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Trello connection is required');
  }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'TrelloListCardsFlow'; }
  /** Register validation, provider, and completion Steps. */
  getSteps(): StepList<ListCardsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListCardsStep({
        stepType: 'ListTrelloCards',
        annotations: { groupId: 'trello-cards', groupLabel: 'Trello Cards', explanation: 'List one bounded page of Trello board cards.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [cardRequest, cardPage] }; }
}

/** Validate the selected board before the provider Step. */
// dex:group group-id:trello-cards group-label:"Trello Cards"
// dex:explanation text:"Validate the board ID before listing cards."
export class PrepareListCards implements Step<ListCardsInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareTrelloListCards'; }
  /** Save safe input and start the provider Step. */
  execute(context: Context, input: ListCardsInput): StepDecision {
    if (!/^[0-9a-fA-F]{24}$/.test(input.boardId ?? '')) return forceFail('A Trello board ID is required');
    cardRequest.set(context, input);
    return goTo(new StepRef('ListTrelloCards'), input);
  }
}

/** Persist the listed card page and complete the Flow. */
// dex:group group-id:trello-cards group-label:"Trello Cards"
// dex:explanation text:"Persist the returned card page and complete the Flow."
export class CompleteListCards implements Step<QueryResult<ListCardsOutput>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteTrelloListCards'; }
  /** Complete with provider-safe card fields. */
  execute(context: Context, result: QueryResult<ListCardsOutput>): StepDecision {
    cardPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
