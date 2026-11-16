// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  ItemChangeKindSchema, defineItemChangedTriggerBinding, newGetItemStep,
  type Connection, type GetItemInput, type Item, type ItemChangedEvent,
} from '@superdurable/connector-monday';

/** Static Dex Web connection for monday.com. */
export const ConnectionName = 'monday-board-watcher';
/** The stable itemChanged binding that starts this Flow. */
export const ItemChangedTriggerBinding = 'board-item-changes';

/** One itemChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface ItemChangeInput { /** Names the Flow too. */ triggerEventId: string; /** The delivered change. */ change: ItemChangedEvent }
/** The Flow result: the changed item and its state read back from monday.com. */
export interface ItemChangeOutcome { /** The watched board. */ boardId: string; /** The changed item. */ itemId: string; /** The delivered kind. */ changeKind: string; /** Read back from monday.com. */ current: Item }

/** The delivered change. */
export const itemChangeAttribute = new Attribute<ItemChangeInput>('monday-item-change', jsonCodec<ItemChangeInput>());
/** The recorded outcome. */
export const itemChangeOutcomeAttribute = new Attribute<ItemChangeOutcome>('monday-item-change-outcome', jsonCodec<ItemChangeOutcome>());

/** Records one monday.com item change and reads the item back. */
export class ItemChangeFlow implements Flow<ItemChangeInput> {
  private readonly record = new RecordItemChange();
  private readonly complete = new CompleteItemChange();
  /** Bind the static monday.com connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError(`item change Flow requires the ${ConnectionName} connection`);
  }
  /** Stable Flow type. */ getFlowType(): string { return 'MondayItemChangeTypeScript'; }
  /** Start at the record Step, which the Trigger starts the Flow at. */
  getSteps(): StepList<ItemChangeInput> {
    return StepList.startStep(this.record).otherSteps(newGetItemStep<ItemChangeInput>({
      stepType: 'ReadChangedItem', connection: this.connection, connectionName: ConnectionName,
      annotations: { groupId: 'change', groupLabel: 'Change', explanation: "Read the changed item back, so the record holds its current state rather than the poll's copy." },
      mapToOperationInput: mapToGetItemInput, found: this.complete,
    }), this.complete);
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [itemChangeAttribute, itemChangeOutcomeAttribute] }; }
  /** Declare the itemChanged binding and its board field for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineItemChangedTriggerBinding({
      connectionName: 'monday-board-watcher', bindingName: 'board-item-changes',
      configurationUI: { units: [{
        id: 'watchedBoard', unitId: 'boardIdInput', label: 'Watched board', required: true,
        description: "Enter the numeric ID of the board whose created and changed items start this Flow: open the board at https://monday.com/ and copy the digits after /boards/ in its address, such as 1234567890, or paste the whole address. The ID is not secret. The connection's user must be able to read the board. Blank saves nothing, and the Worker refuses to start without a board. The first start records the board's items as a baseline and starts no Flow for them. Restart the Worker after saving; a changed board records a new baseline.",
        bindings: [{ port: 'boardId', jsonPointer: '/boardId' }],
      }] },
    })];
  }
}

/** Name one Flow per change, so a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<ItemChangedEvent>): string { return `monday-item-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<ItemChangedEvent>): ItemChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every change; an application narrows it, for example to one group. */
export function acceptEveryItemChange(): boolean { return true; }
/** Read the changed item by its ID. */
export function mapToGetItemInput(input: ItemChangeInput): GetItemInput { return { itemId: input.change.item.id }; }

/** Return why a delivered change is unusable, or '' when it is valid. */
export function validateItemChangeInput(input: ItemChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.boardId?.trim()) return 'change.boardId is required';
  if (!input.change.item?.id?.trim()) return 'change.item.id is required';
  if (!ItemChangeKindSchema.safeParse(input.change.changeKind).success) return 'change.changeKind must be created or updated';
  return '';
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered item change, then read the item back."
class RecordItemChange implements Step<ItemChangeInput> {
  getStepType(): string { return 'RecordItemChange'; }
  execute(context: Context, input: ItemChangeInput): StepDecision {
    const problem = validateItemChangeInput(input);
    if (problem) return forceFail(problem);
    itemChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedItem'), input);
  }
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the item read back beside the delivered change, and complete."
class CompleteItemChange implements Step<QueryResult<Item>> {
  getStepType(): string { return 'CompleteItemChange'; }
  execute(context: Context, result: QueryResult<Item>): StepDecision {
    const change = itemChangeAttribute.get(context);
    const outcome: ItemChangeOutcome = { boardId: change.change.boardId, itemId: change.change.item.id, changeKind: change.change.changeKind, current: result.value };
    itemChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
