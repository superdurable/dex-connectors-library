// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  CardActionDeleted, WatchedCardActionTypes,
  defineCardChangedTriggerBinding, newGetCardStep, type Card, type CardActionType, type CardChangedEvent, type Connection, type GetCardInput,
} from '@superdurable/connector-atlassian-trello';

/** Stable Flow identity each card action starts. */
export const FlowType = 'TrelloCardChangeTypeScript';
/** Static Dex Web connection for Trello. */
export const ConnectionName = 'trello-card-watcher';
/** The cardChanged binding that starts this Flow. */
export const CardChangedTriggerBinding = 'board-card-changes';

/** One cardChanged delivery and the stable Trigger event ID that also names the Flow. */
export interface CardChangeInput { /** Trigger event ID. */ triggerEventId: string; /** The delivered card action. */ change: CardChangedEvent }
/** The Flow result: the delivered action and the card read back, absent for a deleted card. */
export interface CardChangeOutcome {
  /** The board the action belongs to. */ boardId: string;
  /** The changed card. */ cardId: string;
  /** The delivered Trello action type. */ actionType: CardActionType;
  /** The card read back from Trello; absent for a deleted card and for a card deleted after the action. */ current?: Card;
}

/** The delivered card action. */
export const cardChangeAttribute = new Attribute<CardChangeInput>('trello-card-change', jsonCodec<CardChangeInput>());
/** The recorded change outcome. */
export const cardChangeOutcomeAttribute = new Attribute<CardChangeOutcome>('trello-card-change-outcome', jsonCodec<CardChangeOutcome>());

/** Records one Trello card action and reads the card back. */
export class CardChangeFlow implements Flow<CardChangeInput> {
  private readonly record = new RecordCardChange();
  private readonly complete = new CompleteCardChange();
  private readonly removed = new RecordCardRemovedSince();
  /** Bind the Trello connection at registration time. */ constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Trello connection is required'); }
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return FlowType; }
  /** Register the record, read, and completion Steps. */
  getSteps(): StepList<CardChangeInput> {
    return StepList.startStep(this.record).otherSteps(
      newGetCardStep<CardChangeInput>({
        stepType: 'ReadChangedCard', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed card back, so the record holds its current state rather than the action snapshot.' },
        mapToOperationInput: mapToGetCardInput, found: this.complete,
        // A card deleted after its action still completes, recorded without a current state.
        notFound: this.removed,
      }),
      this.complete,
      this.removed,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [cardChangeAttribute, cardChangeOutcomeAttribute] }; }
  /** Declare the cardChanged binding and its board input for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineCardChangedTriggerBinding({ connectionName: ConnectionName, bindingName: CardChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedBoard', unitId: 'boardInput', label: 'Watched board', required: true,
      description: "Open the board at https://trello.com as the connection's member and paste its address, https://trello.com/b/<short link>/<board name>; the unit saves the 8-character short link, or a 24-character board ID pasted instead. It is not secret. Every card created, updated, commented on, moved in, or deleted on that board starts this Flow. The board is required: leaving it blank stops the Worker at start. The first start records earlier actions as a baseline and starts no Flow for them. Restart the Worker after saving; a changed board records a new baseline.",
      bindings: [{ port: 'boardId', jsonPointer: '/boardId' }],
    }] } })];
  }
}

/** Name one Flow per card action; a redelivered action finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<CardChangedEvent>): string { return `trello-card-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered card action. */
export function mapToFlowInput(event: TriggerEvent<CardChangedEvent>): CardChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every card action; an application narrows it, for example to cards moved into one list or to comments. */
export function acceptEveryCardChange(): boolean { return true; }
/** Read the changed card. */
export function mapToGetCardInput(input: CardChangeInput): GetCardInput { return { cardId: input.change.card.id }; }

/** Return why a delivered card action is unusable, or an empty string. */
export function validateCardChange(input: CardChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.actionId?.trim() || !input.change.card?.id?.trim()) return 'change.actionId and change.card.id are required';
  if (!(WatchedCardActionTypes as readonly string[]).includes(input.change.actionType)) return 'change.actionType must be a Trello card action type such as createCard or updateCard';
  return '';
}

function outcomeOf(input: CardChangeInput, current?: Card): CardChangeOutcome {
  return { boardId: input.change.boardId, cardId: input.change.card.id, actionType: input.change.actionType, ...(current ? { current } : {}) };
}

/** Validate and record the delivered card action. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered card action, completing at once for a deleted card and otherwise reading the card back."
export class RecordCardChange implements Step<CardChangeInput> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordCardChange'; }
  /** Fail an unusable action; complete a deletion; otherwise record it and read the card. */
  execute(context: Context, input: CardChangeInput): StepDecision {
    const problem = validateCardChange(input);
    if (problem) return forceFail(problem);
    cardChangeAttribute.set(context, input);
    if (input.change.actionType === CardActionDeleted) {
      const outcome = outcomeOf(input);
      cardChangeOutcomeAttribute.set(context, outcome);
      return gracefulComplete(outcome);
    }
    return goTo(new StepRef('ReadChangedCard'), input);
  }
}

/** Record the card read back as the outcome. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the card read back from Trello as the outcome and complete the Flow."
export class CompleteCardChange implements Step<QueryResult<Card>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteCardChange'; }
  /** Save the outcome and complete with it. */
  execute(context: Context, result: QueryResult<Card>): StepDecision {
    const outcome = outcomeOf(cardChangeAttribute.get(context), result.value);
    cardChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}

/** Record a card deleted after its action. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record a card deleted after its action without a current state and complete the Flow."
export class RecordCardRemovedSince implements Step<QueryResult<Card>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordCardRemovedSince'; }
  /** Save the outcome without a current card and complete. */
  execute(context: Context): StepDecision {
    const outcome = outcomeOf(cardChangeAttribute.get(context));
    cardChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
