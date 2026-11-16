// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  DatabaseRowChangeKind, defineDatabaseRowChangedTriggerBinding, newGetPageStep,
  type Connection, type DatabaseRowChangedEvent, type GetPageInput, type GetPageOutput, type Page,
} from '@superdurable/connector-notion';

/** The static Dex Web connection for Notion. */
export const ConnectionName = 'notion-row-watcher';
/** The stable databaseRowChanged binding that starts this Flow. */
export const DatabaseRowChangedTriggerBinding = 'database-row-changes';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered change finds its Flow. */
export const FlowIDPrefix = 'notion-row-change-';

/** One databaseRowChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface RowChangeInput { /** The Trigger event ID that also names the Flow. */ triggerEventId: string; /** The delivered row change. */ change: DatabaseRowChangedEvent }

/** The Flow result and the value of its outcome Attribute. */
export interface ChangeOutcome {
  /** The watched data source. */ dataSourceId: string;
  /** The changed row's page ID. */ rowId: string;
  /** The delivered change kind. */ changeKind: string;
  /** The row read back from Notion. */ current: Page;
}

/** The delivered change. */
export const rowChangeAttribute = new Attribute<RowChangeInput>('notion-row-change', jsonCodec<RowChangeInput>());
/** The recorded outcome. */
export const rowChangeOutcomeAttribute = new Attribute<ChangeOutcome>('notion-row-change-outcome', jsonCodec<ChangeOutcome>());

/** Records one Notion database row change, reading the row back. */
export class DatabaseRowChangeFlow implements Flow<RowChangeInput> {
  private readonly record = new RecordRowChange();
  private readonly complete = new CompleteRowChange();
  /** Bind the Notion connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('row change Flow requires the notion-row-watcher connection');
  }
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'NotionDatabaseRowChangeTypeScript'; }
  /** Record the change, read the row back, and complete. */
  getSteps(): StepList<RowChangeInput> {
    return StepList.startStep(this.record).otherSteps(
      newGetPageStep<RowChangeInput>({
        stepType: 'ReadChangedRow', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed row back, so the record holds its current properties rather than the polled snapshot.' },
        mapToOperationInput: mapToGetPageInput, found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [rowChangeAttribute, rowChangeOutcomeAttribute] }; }
  /** Declare the databaseRowChanged binding and its data source picker for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineDatabaseRowChangedTriggerBinding({ connectionName: ConnectionName, bindingName: DatabaseRowChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedDataSource', unitId: 'dataSourcePicker', label: 'Watched database', required: true,
      description: "Choose the Notion data source whose created and edited rows start this Flow: paste the database link from its ••• menu > Copy link at https://app.notion.com and pick a data source, or paste a data source ID from the database's settings > Manage data sources > Copy data source ID. Share the database with this connection first. The picker saves the data source ID and name; the binding needs one, so the Worker does not start until it is saved. The first start records existing rows as a baseline and starts no Flow for them. Restart the Worker after saving; a changed data source records a new baseline.",
      bindings: [
        { port: 'dataSourceId', jsonPointer: '/dataSourceId' },
        { port: 'dataSourceName', jsonPointer: '/dataSourceName' },
      ],
    }] } })];
  }
}

/** Name one Flow per row version; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<DatabaseRowChangedEvent>): string { return FlowIDPrefix + event.id; }

/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<DatabaseRowChangedEvent>): RowChangeInput { return { triggerEventId: event.id, change: event.payload }; }

/** Admit every change; an application takes the event and narrows it, for example to created rows. */
export function acceptEveryRowChange(): boolean { return true; }

/** Read the changed row's properties without its body blocks. */
export function mapToGetPageInput(input: RowChangeInput): GetPageInput { return { pageId: input.change.row.id, skipContent: true }; }

/** Return why a delivered change cannot be recorded, or an empty string. */
export function validateRowChangeInput(input: RowChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.dataSourceId?.trim() || !input.change.row?.id?.trim()) return 'change.dataSourceId and change.row.id are required';
  if (!(Object.values(DatabaseRowChangeKind) as string[]).includes(input.change.changeKind)) return 'change.changeKind must be created or updated';
  return '';
}

/** Store the delivered change before the row is read back. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered row change, then read the row back from Notion."
export class RecordRowChange implements Step<RowChangeInput> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordRowChange'; }
  /** Fail an invalid delivery; otherwise record it and read the row. */
  execute(context: Context, input: RowChangeInput): StepDecision {
    const problem = validateRowChangeInput(input);
    if (problem) return forceFail(problem);
    rowChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedRow'), input);
  }
}

/** Complete with the change and the row's current properties. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the row read back from Notion as the outcome and complete the Flow."
export class CompleteRowChange implements Step<QueryResult<GetPageOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteRowChange'; }
  /** Save and return the outcome. */
  execute(context: Context, result: QueryResult<GetPageOutput>): StepDecision {
    const { change } = rowChangeAttribute.get(context);
    const outcome: ChangeOutcome = { dataSourceId: change.dataSourceId, rowId: change.row.id, changeKind: change.changeKind, current: result.value.page };
    rowChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
