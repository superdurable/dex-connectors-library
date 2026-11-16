// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  RecordChangeCreated, RecordChangeUpdated,
  defineRecordModifiedPolledTriggerBinding, newGetRecordStep,
  type Connection, type GetRecordInput, type Record as SalesforceRecord, type RecordChangeKind, type RecordModifiedEvent,
} from '@superdurable/connector-salesforce';

/** The static Dex Web connection for Salesforce, shared with the record-lookup example. */
export const ConnectionName = 'salesforce-crm';
/** The stable recordModifiedPolled binding that starts this Flow. */
export const RecordModifiedTriggerBinding = 'record-changes';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered change finds its existing Flow. */
export const FlowIDPrefix = 'salesforce-record-change-';

/** One recordModifiedPolled delivery. */
export interface RecordChange {
  /** The stable Trigger event ID that also names the Flow. */ triggerEventId: string;
  /** The delivered record change. */ change: RecordModifiedEvent;
}

/** The Flow result and the value of its outcome Attribute. */
export interface ChangeOutcome {
  /** The watched object. */ sObjectType: string;
  /** The changed record. */ recordId: string;
  /** The delivered change kind. */ changeKind: RecordChangeKind;
  /** Whether the record was deleted before the Flow read it back. */ isDeleted: boolean;
  /** The record read back from Salesforce; omitted when isDeleted is true. */ current?: SalesforceRecord;
}

/** The delivered change. */
export const recordChangeAttribute = new Attribute<RecordChange>('salesforce-record-change', jsonCodec<RecordChange>());
/** The recorded outcome. */
export const recordChangeOutcomeAttribute = new Attribute<ChangeOutcome>('salesforce-record-change-outcome', jsonCodec<ChangeOutcome>());

/** Records one Salesforce record change: it reads the record back, or records that it was deleted first. */
export class RecordChangeFlow implements Flow<RecordChange> {
  private readonly recordChange = new RecordRecordChange();
  private readonly completeChange = new CompleteRecordChange();
  private readonly completeDeleted = new CompleteDeletedRecord();
  /** Bind the Salesforce connection that reads changed records. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'SalesforceRecordChangeTypeScript'; }
  /** Register the record, read, and outcome Steps. */
  getSteps(): StepList<RecordChange> {
    return StepList.startStep(this.recordChange).otherSteps(
      newGetRecordStep<RecordChange>({
        stepType: 'ReadChangedRecord', connection: this.connection, connectionName: 'salesforce-crm',
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed record back, so the outcome holds its current values rather than the polled snapshot.' },
        mapToOperationInput: mapToGetRecordInput,
        found: this.completeChange,
        // Business reaction: a record deleted after the poll read it completes as deleted instead of failing.
        notFound: this.completeDeleted,
      }),
      this.completeChange,
      this.completeDeleted,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [recordChangeAttribute, recordChangeOutcomeAttribute] }; }
  /** Declare the recordModifiedPolled binding with its object picker and field list for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineRecordModifiedPolledTriggerBinding({ connectionName: 'salesforce-crm', bindingName: 'record-changes', configurationUI: { units: [
      {
        id: 'watchedObject', unitId: 'sObjectPicker', label: 'Watched object', required: true,
        description: 'Choose the object whose created and modified records start this Flow, such as Opportunity, or sign in at https://login.salesforce.com (https://test.salesforce.com for a sandbox) and enter a custom object API name from Setup > Object Manager > the object > API Name, such as Invoice__c; the unit saves the non-secret object API name, and it is required. The first start records existing records as a baseline and starts no Flow for them. Restart the Worker after saving; a changed object records a new baseline.',
        bindings: [{ port: 'sObjectType', jsonPointer: '/sObjectType' }],
      },
      {
        id: 'deliveredFields', unitId: 'fieldNamesInput', label: 'Delivered fields', required: false,
        description: 'Enter the comma-separated field API names each delivered change carries and the Flow reads back, such as Name, StageName, after signing in at https://login.salesforce.com (https://test.salesforce.com for a sandbox) under Setup > Object Manager > the object > Fields & Relationships > Field Name column; the unit saves the non-secret list. Blank delivers only Id, CreatedDate, LastModifiedDate, and SystemModstamp. Restart the Worker after saving; the cursor is kept.',
        bindings: [{ port: 'fieldNames', jsonPointer: '/fieldNames' }],
      },
    ] } })];
  }
}

/** Admit every change; an application narrows it, for example to created records. */
export function acceptEveryRecordChange(): boolean { return true; }

/** Name one Flow per change; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<RecordModifiedEvent>): string { return FlowIDPrefix + event.id; }

/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<RecordModifiedEvent>): RecordChange { return { triggerEventId: event.id, change: event.payload }; }

/** Read back every field the delivered change carries, in a stable order. */
export function mapToGetRecordInput(input: RecordChange): GetRecordInput {
  const fields = Object.keys(input.change.record.fields ?? {}).sort();
  return { sObjectType: input.change.sObjectType, recordId: input.change.recordId, fields };
}

/** Validate and persist the delivered change. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered record change, then read the record back from Salesforce."
export class RecordRecordChange implements Step<RecordChange> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordRecordChange'; }
  /** Store the change and read the record back. */
  execute(context: Context, input: RecordChange): StepDecision {
    if (!input.triggerEventId?.trim()) return forceFail('triggerEventId is required');
    if (!input.change?.sObjectType?.trim() || !input.change.recordId?.trim()) return forceFail('change.sObjectType and change.recordId are required');
    if (input.change.changeKind !== RecordChangeCreated && input.change.changeKind !== RecordChangeUpdated) return forceFail('change.changeKind must be created or updated');
    recordChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedRecord'), input);
  }
}

/** Persist the record read back and complete. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the record read back from Salesforce as the outcome and complete the Flow."
export class CompleteRecordChange implements Step<QueryResult<SalesforceRecord>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteRecordChange'; }
  /** Store the outcome and complete the Flow with it. */
  execute(context: Context, result: QueryResult<SalesforceRecord>): StepDecision {
    const { change } = recordChangeAttribute.get(context);
    const outcome: ChangeOutcome = { sObjectType: change.sObjectType, recordId: change.recordId, changeKind: change.changeKind, isDeleted: false, current: result.value };
    recordChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}

/** Record a deletion observed by the read-back and complete. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record that the record was deleted before the read-back and complete the Flow."
export class CompleteDeletedRecord implements Step<QueryResult<SalesforceRecord>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteDeletedRecord'; }
  /** Store a deleted outcome and complete the Flow with it. */
  execute(context: Context): StepDecision {
    const { change } = recordChangeAttribute.get(context);
    const outcome: ChangeOutcome = { sObjectType: change.sObjectType, recordId: change.recordId, changeKind: change.changeKind, isDeleted: true };
    recordChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
