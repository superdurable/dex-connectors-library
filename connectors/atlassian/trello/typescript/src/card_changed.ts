// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { MemberReferenceSchema } from './generated/models.js';

/** createCard: a card was added to a list on the board. */ export const CardActionCreated = 'createCard';
/** copyCard: a card was created as a copy of another card. */ export const CardActionCopied = 'copyCard';
/** convertToCardFromCheckItem: a checklist item became a card. */ export const CardActionConvertedFromCheckItem = 'convertToCardFromCheckItem';
/** emailCard: a card was created by email to the board. */ export const CardActionEmailed = 'emailCard';
/** updateCard: a field changed, such as the list (a move), name, due date, or closed; changedFields names them. */ export const CardActionUpdated = 'updateCard';
/** commentCard: a member commented on a card; commentText holds the comment. */ export const CardActionCommented = 'commentCard';
/** moveCardToBoard: a card arrived from another board. */ export const CardActionMovedToBoard = 'moveCardToBoard';
/** deleteCard: a card was deleted; Trello keeps only its ID, short ID, and list. */ export const CardActionDeleted = 'deleteCard';
/** The Trello action types every cardChanged poll requests, in the Go connector's filter order. */
export const WatchedCardActionTypes = [
  CardActionCreated, CardActionCopied, CardActionConvertedFromCheckItem, CardActionEmailed,
  CardActionUpdated, CardActionCommented, CardActionMovedToBoard, CardActionDeleted,
] as const;
/** The Trello action type of one cardChanged delivery. */
export type CardActionType = typeof WatchedCardActionTypes[number];

/** Trello board input UI unit, which stores a board short link or ID. */ export const UIUnitBoardInput = 'boardInput';
/** Board input output port: the board's short link or 24-character ID. */ export const UIBoardInputPortBoardID = 'boardId';

const boardIdPattern = /^[0-9a-fA-F]{24}$/;
const shortLinkPattern = /^[0-9A-Za-z]{8}$/;

/**
 * The cardChanged binding configuration that the boardInput unit saves in Dex Web; field names match the Go
 * connector. Changing the board of a binding that already polled makes its next poll record a new baseline.
 */
export const CardChangedTriggerConfigurationSchema = z.strictObject({
  /** The watched board: its 24-character ID or its 8-character short link from https://trello.com/b/<shortLink>/<name>. Required. */
  boardId: z.string().optional(),
}).superRefine((configuration, context) => {
  const boardId = configuration.boardId ?? '';
  if (!boardIdPattern.test(boardId) && !shortLinkPattern.test(boardId)) {
    context.addIssue({ code: 'custom', message: 'cardChanged boardId must be a 24-character Trello board ID or an 8-character board short link' });
  }
});
/** The watched board of one cardChanged binding. */
export type CardChangedTriggerConfiguration = z.infer<typeof CardChangedTriggerConfigurationSchema>;

/** A list as a Trello action records it. */
export const ActionListSchema = z.strictObject({
  /** The list's 24-character ID. */ id: z.string(),
  /** The list name when the action recorded it. */ name: z.string().optional(),
});
/** A list as a Trello action records it. */
export type ActionList = z.infer<typeof ActionListSchema>;

/** The card snapshot a Trello action records. */
export const ActionCardSchema = z.strictObject({
  /** The card's 24-character ID. */ id: z.string(),
  /** The card name when the action recorded it; absent for a deleted card. */ name: z.string().optional(),
  /** The card's 8-character short link, when recorded. */ shortLink: z.string().optional(),
  /** The card's number on its board, such as 42, when recorded. */ shortId: z.number().int().optional(),
  /** The card's list, when recorded. */ listId: z.string().optional(),
  /** The archived state an updateCard recorded; absent when the action did not record it. */ isClosed: z.boolean().optional(),
});
/** The card snapshot a Trello action records. */
export type ActionCard = z.infer<typeof ActionCardSchema>;

/**
 * One card action read from the watched board's action history; field names and order match the Go
 * connector, so a Go and a TypeScript replica share an inbox. Read the card with getCard for its current state.
 */
export const CardChangedEventSchema = z.strictObject({
  /** The board the action belongs to, as Trello reports its 24-character ID. */ boardId: z.string(),
  /** The board's name when the action recorded it. */ boardName: z.string().optional(),
  /** Trello's 24-character action ID; it also identifies the Trigger event. */ actionId: z.string(),
  /** The Trello action type. */ actionType: z.enum(WatchedCardActionTypes),
  /** When Trello recorded the action, as an RFC 3339 UTC time with millisecond precision. */ occurredAt: z.string(),
  /** The ID of the member who performed the action. */ memberCreatorId: z.string().optional(),
  /** That member's name and username when Trello returned them. */ memberCreator: MemberReferenceSchema.optional(),
  /** The card as the action recorded it. */ card: ActionCardSchema,
  /** The card's list when the action recorded one, such as for createCard. */ list: ActionListSchema.optional(),
  /** The list an updateCard moved the card from. */ listBefore: ActionListSchema.optional(),
  /** The list an updateCard moved the card to. */ listAfter: ActionListSchema.optional(),
  /** The card fields an updateCard changed, such as idList, name, due, or closed, sorted. */ changedFields: z.array(z.string()).optional(),
  /** The comment of a commentCard action. */ commentText: z.string().optional(),
});
/** One card action read from the watched board's action history. */
export type CardChangedEvent = z.infer<typeof CardChangedEventSchema>;

/** Manifest definition of cardChanged. */
export const CardChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'cardChanged' },
  description: "Receive each card created, copied, updated (moved, renamed, archived, rescheduled, and other field changes), commented on, moved in from another board, or deleted on one board, read by polling the board's action history; the first run records a baseline and delivers no earlier actions.",
};

/** One static application binding of cardChanged. */
export interface CardChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the boardInput unit. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static cardChanged binding for Flow Definition tooling and Dex Web. */
export function defineCardChangedTriggerBinding(config: CardChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: CardChangedTriggerDefinition, ...config });
}

/** Return the Trigger event ID of one action, a pure function of its ID, so a refetch yields the same ID. */
export function cardChangeEventId(actionId: string): string { return `trello-action-${actionId.toLowerCase()}`; }
