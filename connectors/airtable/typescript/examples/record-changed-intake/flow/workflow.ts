// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineRecordChangedTriggerBinding, newGetRecordStep, type Connection, type GetRecordInput, type Record as AirtableRecord, type RecordChangedEvent } from '@superdurable/connector-airtable';

/** The stable Flow identity shown in Dex Web. */
export const FlowType = 'AirtableRecordChangedIntakeTypeScript';
/** The static Dex Web connection whose table the Trigger polls and the Flow reads the record back from. */
export const ConnectionName = 'airtable-intake';
/** The recordChanged binding whose changes start this Flow. */
export const RecordChangedTriggerBinding = 'intake-record-changed';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered change maps to one Flow. */
export const FlowIDPrefix = 'airtable-record-change-';

/** The Flow's start input: one change the recordChanged Trigger found. */
export interface ChangedRecord {
  /** The Trigger event ID, such as recXXXXXXXXXXXXXX-20261115T090000123000000Z. */ eventId: string;
  /** The polled base ID. */ baseId: string;
  /** The polled table ID. */ tableId: string;
  /** The changed record's ID. */ recordId: string;
  /** Whether the record was created and not edited since. */ isCreatedRecord: boolean;
  /** The record's Last modified time value, as RFC 3339 UTC text. */ lastModifiedTime: string;
  /** Each non-empty field name and its cell value. */ fields: { [fieldName: string]: unknown };
}

/** The Flow's state and output: the reported change and the record as the Flow read it back. */
export interface Intake {
  /** The change the Trigger reported. */ change: ChangedRecord;
  /**
   * Each non-empty field name and its cell value when the Flow read the record back; absent until then and
   * when the record was deleted.
   */
  currentFields?: { [fieldName: string]: unknown };
  /** Whether the record was deleted after the change and before the Flow read it back. */ isRemoved: boolean;
  /** Whether the Flow finished reading the record back. */ isCompleted: boolean;
}

/** The recorded change and read-back record. */
export const intakeAttribute = new Attribute<Intake>('airtable-record-intake', jsonCodec<Intake>());

/** Records one changed Airtable record and reads it back through the airtable-intake connection. */
export class RecordChangedIntakeFlow implements Flow<ChangedRecord> {
  private readonly record = new RecordChangedRecord();
  private readonly complete = new CompleteRecordIntake();
  private readonly removed = new RecordRemovedRecord();

  /** Bind the connection named ConnectionName, the one whose table the Trigger polls. */
  constructor(private readonly connection: Connection) {
    if (connection?.reference.name !== ConnectionName) throw new TypeError('record-changed intake requires the static airtable-intake connection');
  }

  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'AirtableRecordChangedIntakeTypeScript'; }
  /** Record the change, read the record back, and complete with its current fields or as removed. */
  getSteps(): StepList<ChangedRecord> {
    return StepList.startStep(this.record).otherSteps(
      newGetRecordStep({
        stepType: 'ReadBackChangedRecord',
        annotations: { groupId: 'airtable', groupLabel: 'Airtable', explanation: 'Read the changed record back by its ID to capture its current fields.' },
        connection: this.connection, connectionName: 'airtable-intake',
        mapToOperationInput: mapToReadBackInput,
        found: this.complete, notFound: this.removed,
      }),
      this.complete,
      this.removed,
    );
  }
  /** Register the intake Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [intakeAttribute] }; }
  /** Declare the recordChanged binding and its base, table, and Last modified time field pickers for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineRecordChangedTriggerBinding({ connectionName: 'airtable-intake', bindingName: 'intake-record-changed', configurationUI: { units: [
      {
        id: 'intakeBase', unitId: 'basePicker', label: 'Base', required: true,
        description: "Select the base that holds the table to watch; Load bases lists the bases the personal access token can reach, and the picker stores the base ID and name. To enter the ID instead, copy the app followed by 14 letters and digits from the base's https://airtable.com/ address. It cannot be blank.",
        bindings: [{ port: 'baseId', jsonPointer: '/baseId' }, { port: 'baseName', jsonPointer: '/baseName' }],
      },
      {
        id: 'intakeTable', unitId: 'tablePicker', label: 'Table', required: true,
        description: 'Select the table whose created and changed records start this Flow, after saving the base; the picker stores the table ID, which keeps working after a rename, and the table name. Records already in the table when the Worker first polls never start Flows. It cannot be blank.',
        bindings: [{ port: 'baseId', jsonPointer: '/baseId' }, { port: 'tableId', jsonPointer: '/tableId' }, { port: 'tableName', jsonPointer: '/tableName' }],
      },
      {
        id: 'intakeLastModified', unitId: 'lastModifiedTimeFieldPicker', label: 'Last modified time field', required: true,
        description: 'Select the table\'s Last modified time field, after saving the table; the picker stores the field ID and name. If the table has none, add a field of type Last modified time with Include time turned on in Airtable. Its setting decides what counts as a change: all fields, or only the fields it watches, so a Status-only field starts a Flow only when Status changes. It cannot be blank.',
        bindings: [
          { port: 'baseId', jsonPointer: '/baseId' }, { port: 'tableId', jsonPointer: '/tableId' },
          { port: 'fieldId', jsonPointer: '/fieldId' }, { port: 'fieldName', jsonPointer: '/fieldName' },
        ],
      },
    ] } })];
  }
}

/** The application's admission rule: every change the Trigger reports starts a Flow. */
export function acceptChangedRecord(): boolean { return true; }

/** Derive the Flow ID from the event ID, so every redelivery of a change maps to one Flow. */
export function resolveFlowId(event: TriggerEvent<RecordChangedEvent>): string { return FlowIDPrefix + event.id; }

/** Copy the changed record into the Flow's start input. */
export function mapToFlowInput(event: TriggerEvent<RecordChangedEvent>): ChangedRecord {
  const change = event.payload;
  return {
    eventId: event.id, baseId: change.baseId, tableId: change.tableId, recordId: change.record.id,
    isCreatedRecord: change.isCreatedRecord, lastModifiedTime: change.lastModifiedTime, fields: change.record.fields ?? {},
  };
}

/** Read the changed record from its polled base and table. */
export function mapToReadBackInput(change: ChangedRecord): GetRecordInput {
  return { baseId: change.baseId, tableIdOrName: change.tableId, recordId: change.recordId };
}

/** Store the read-back record's fields; throws TypeError when it is not the changed record. */
export function completeWithCurrentRecord(intake: Intake, record: AirtableRecord): Intake {
  if (record.id !== intake.change.recordId) throw new TypeError(`Airtable returned record ${record.id} instead of the changed record ${intake.change.recordId}`);
  return { ...intake, currentFields: record.fields ?? {}, isCompleted: true };
}

/** Persist the changed record, then read it back. */
// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Persist the changed Airtable record, then read it back from Airtable."
export class RecordChangedRecord implements Step<ChangedRecord> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordChangedRecord'; }
  /** Fail an incomplete delivery; otherwise save the change and read the record back. */
  execute(context: Context, change: ChangedRecord): StepDecision {
    if (!change.eventId || !change.recordId || !change.lastModifiedTime) return forceFail('a changed record requires its event ID, record ID, and modification time');
    intakeAttribute.set(context, { change, isRemoved: false, isCompleted: false });
    return goTo(new StepRef('ReadBackChangedRecord'), change);
  }
}

/** Store the read-back record and complete. */
// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Store the record's current fields and complete the Flow."
export class CompleteRecordIntake implements Step<QueryResult<AirtableRecord>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteRecordIntake'; }
  /** Complete with the record's current fields, or fail when Airtable returned another record. */
  execute(context: Context, result: QueryResult<AirtableRecord>): StepDecision {
    let completed: Intake;
    try { completed = completeWithCurrentRecord(intakeAttribute.get(context), result.value); }
    catch (error) { return forceFail(error instanceof Error ? error.message : 'Airtable returned another record'); }
    intakeAttribute.set(context, completed);
    return gracefulComplete(completed);
  }
}

/** Record that the changed record is gone and complete. */
// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Mark the record as deleted since the change and complete the Flow."
export class RecordRemovedRecord implements Step<QueryResult<AirtableRecord>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordRemovedRecord'; }
  /** Complete as removed. */
  execute(context: Context): StepDecision {
    const removed: Intake = { ...intakeAttribute.get(context), isRemoved: true, isCompleted: true };
    intakeAttribute.set(context, removed);
    return gracefulComplete(removed);
  }
}
