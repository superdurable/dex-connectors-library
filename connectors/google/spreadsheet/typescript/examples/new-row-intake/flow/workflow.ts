// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineRowAddedTriggerBinding, newGetValuesStep, type Connection, type GetValuesInput, type GetValuesOutput, type RowAddedEvent } from '@superdurable/connector-google-sheets';

/** Static Dex Web connection whose tab the Trigger polls. */
export const ConnectionName = 'google-sheets-intake';
/** The rowAdded binding whose rows start this Flow. */
export const NewRowTriggerBinding = 'new-intake-row';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered row maps to one Flow. */
export const FlowIDPrefix = 'google-sheets-row-';

/** The Flow's start input: one row the rowAdded Trigger found. */
export interface AddedRow {
  /** Trigger event ID, such as row-12-0f3a9c1d2b4e5f60. */ eventId: string;
  /** The polled spreadsheet ID. */ spreadsheetId: string;
  /** The polled tab title. */ sheetName: string;
  /** The sheet row number when the row was read. */ rowNumber: number;
  /** Maps the tab's header cells to the row's formatted cells. */ values: Record<string, string>;
  /** When the poll read the row, as RFC 3339 UTC. */ detectedAt: string;
}

/** The durable result of one intake and the Flow's completion output. */
export interface Outcome {
  /** The row the Trigger reported. */ row: AddedRow;
  /**
   * The row's formatted cells read back from Google Sheets when the Flow ran; they differ from row when the
   * row was edited, or rows above it moved, after the poll. Absent until read.
   */
  currentCells?: string[];
}

/** The recorded row and, once read, its current cells. */
export const addedRowAttribute = new Attribute<Outcome>('google-sheets-added-row', jsonCodec<Outcome>());

/** Records one row appended to a picked Google Sheets tab and reads it back; an application would create a lead here. */
export class NewRowIntakeFlow implements Flow<AddedRow> {
  private readonly recordAddedRow = new RecordAddedRow();
  private readonly completeRowIntake = new CompleteRowIntake();
  /** Bind the Google Sheets connection the read-back Step uses. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Google Sheets connection is required'); }
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'GoogleSheetsNewRowIntake'; }
  /** Register the record, Google Sheets read, and completion Steps. */
  getSteps(): StepList<AddedRow> {
    return StepList.startStep(this.recordAddedRow).otherSteps(
      newGetValuesStep({
        stepType: 'ReadAddedRow',
        annotations: { groupId: 'google-sheets', groupLabel: 'Google Sheets', explanation: "Read the added row's current cells back from the tab." },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: mapToGetValuesInput,
        read: this.completeRowIntake,
      }),
      this.completeRowIntake,
    );
  }
  /** Register the intake outcome Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [addedRowAttribute] }; }
  /** Declare the rowAdded binding that starts this Flow, with the spreadsheetPicker and sheetTabPicker units; Dex Web reads these literals statically. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineRowAddedTriggerBinding({ connectionName: ConnectionName, bindingName: NewRowTriggerBinding, configurationUI: { units: [{
      id: 'spreadsheet', unitId: 'spreadsheetPicker', label: 'Spreadsheet', required: true,
      description: "Select the spreadsheet whose new rows start this Flow. Choose spreadsheet opens the Google Picker, which grants this app the drive.file access the Trigger polls with, and the unit stores the spreadsheet ID. To enter the ID instead, copy the non-secret text between /d/ and /edit in the spreadsheet's https://docs.google.com/spreadsheets/d/ address; it works only for a spreadsheet already granted to this app. It cannot be blank.",
      bindings: [{ port: 'spreadsheetId', jsonPointer: '/spreadsheetId' }],
    }, {
      id: 'tab', unitId: 'sheetTabPicker', label: 'Tab', required: true,
      description: "Select the tab to watch; the picker loads the chosen spreadsheet's current tab titles and stores the exact title. Row 1 must hold the column headers. Rows already on the tab when the Worker first polls never start Flows, and renaming the tab later stops the Trigger until you pick it again. It cannot be blank.",
      bindings: [{ port: 'spreadsheetId', jsonPointer: '/spreadsheetId' }, { port: 'tab', jsonPointer: '/sheetName' }],
    }] } })];
  }
}

/** The application's admission rule: every row the Trigger reports, which is always below the header, starts a Flow. */
export function acceptAddedRow(event: TriggerEvent<RowAddedEvent>): boolean { return event.payload.rowNumber > 1; }

/** Derive the Flow ID from the event ID, so every redelivery of a row maps to one Flow. */
export function resolveFlowId(event: TriggerEvent<RowAddedEvent>): string { return FlowIDPrefix + event.id; }

/** Copy the added row into the Flow's start input. */
export function mapToFlowInput(event: TriggerEvent<RowAddedEvent>): AddedRow {
  const row = event.payload;
  return { eventId: event.id, spreadsheetId: row.spreadsheetId, sheetName: row.sheetName, rowNumber: row.rowNumber, values: row.values, detectedAt: event.occurredAt.toISOString() };
}

/** Read the reported row of the polled tab as one whole-row A1 range, such as 'Leads'!7:7; a single quote in the tab title is doubled, as A1 notation requires. */
export function mapToGetValuesInput(row: AddedRow): GetValuesInput {
  return { spreadsheetId: row.spreadsheetId, range: `'${row.sheetName.replaceAll("'", "''")}'!${row.rowNumber}:${row.rowNumber}` };
}

/** Persist the header-keyed row, then read it back. */
// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Persist the added spreadsheet row the rowAdded Trigger reported."
export class RecordAddedRow implements Step<AddedRow> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordAddedRow'; }
  /** Store the row, or fail a row without its identity. */
  execute(context: Context, row: AddedRow): StepDecision {
    if (!row.eventId || !row.spreadsheetId || row.rowNumber < 2) return forceFail('an added row requires its event ID, spreadsheet ID, and a row number below the header');
    addedRowAttribute.set(context, { row });
    return goTo(new StepRef('ReadAddedRow'), row);
  }
}

/** Store the cells read back beside the reported row and complete. */
// dex:group group-id:intake-complete group-label:"Complete intake"
// dex:explanation text:"Record the row's current cells and complete the Flow."
export class CompleteRowIntake implements Step<QueryResult<GetValuesOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteRowIntake'; }
  /** Sheets omits values for a row that is now empty, so the read-back is then an empty cell list. */
  execute(context: Context, result: QueryResult<GetValuesOutput>): StepDecision {
    const outcome: Outcome = { row: addedRowAttribute.get(context).row, currentCells: result.value.values?.[0] ?? [] };
    addedRowAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
