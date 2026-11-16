// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { isAPIName } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import { RecordSchema, type Record as SalesforceRecord } from './generated/models.js';
import { formatGoTime, parseRFC3339 } from './record_poll_cursor.js';

/** Salesforce object picker UI unit, which saves the object API name. */ export const UIUnitSObjectPicker = 'sObjectPicker';
/** Object picker output port. */ export const UISObjectPickerPortSObjectType = 'sObjectType';
/** Comma-separated field API name list UI unit. */ export const UIUnitFieldNamesInput = 'fieldNamesInput';
/** Field name list output port. */ export const UIFieldNamesInputPortFieldNames = 'fieldNames';

/** Bounds the extra fields one recordModifiedPolled binding reads with each record. */
export const MaxTriggerFieldNames = 50;
/** A record whose LastModifiedDate still equals its CreatedDate. */
export const RecordChangeCreated = 'created';
/**
 * Any other change that moved SystemModstamp, including an edit made soon after creation but before the next poll
 * read it, and a system change such as a roll-up summary recalculation that leaves LastModifiedDate unchanged.
 */
export const RecordChangeUpdated = 'updated';
/** The derived kind of one recordModifiedPolled delivery. */
export type RecordChangeKind = typeof RecordChangeCreated | typeof RecordChangeUpdated;
/** Fields selected for every record; they order the poll and derive the change kind. */
export const TriggerSystemFields: readonly string[] = ['Id', 'CreatedDate', 'LastModifiedDate', 'SystemModstamp'];

/**
 * The recordModifiedPolled binding configuration that the object picker and field list save in Dex Web. Changing
 * the object of a binding that already polled makes its next poll record a new baseline for the new object;
 * changing the field list keeps the cursor.
 */
export interface RecordModifiedTriggerConfiguration {
  /** The watched object's API name, such as Opportunity or Invoice__c; it is required. */ sObjectType: string;
  /**
   * Up to MaxTriggerFieldNames extra field API names, such as StageName, read with each record. Id, CreatedDate,
   * LastModifiedDate, and SystemModstamp are always read; empty reads only those.
   */
  fieldNames?: string[] | null | undefined;
}

/**
 * Decodes a saved recordModifiedPolled configuration strictly, as Go does: unknown fields and wrong JSON types are
 * rejected and null is the zero value. validateRecordModifiedTriggerConfiguration checks the values.
 */
export const RecordModifiedTriggerConfigurationSchema: z.ZodType<RecordModifiedTriggerConfiguration> = z.strictObject({
  sObjectType: z.string().nullable().optional().transform(value => value ?? ''),
  fieldNames: z.array(z.string()).nullable().optional(),
});

/** Require an object API name and reject invalid or repeated field API names, as the Go connector's Validate does. */
export function validateRecordModifiedTriggerConfiguration(configuration: RecordModifiedTriggerConfiguration): void {
  if (typeof configuration?.sObjectType !== 'string' || !isAPIName(configuration.sObjectType))
    throw new Error('recordModifiedPolled sObjectType must be an object API name such as Opportunity');
  const fieldNames = configuration.fieldNames ?? [];
  if (fieldNames.length > MaxTriggerFieldNames) throw new Error(`recordModifiedPolled fieldNames must hold at most ${MaxTriggerFieldNames} field API names`);
  const seen = new Set<string>();
  for (const fieldName of fieldNames) {
    if (typeof fieldName !== 'string' || !isAPIName(fieldName))
      throw new Error('recordModifiedPolled fieldNames must be field API names such as StageName, without relationship dots');
    if (seen.has(fieldName.toLowerCase())) throw new Error('recordModifiedPolled fieldNames must not repeat a field API name');
    seen.add(fieldName.toLowerCase());
  }
}

/** The system fields followed by each configured field that is not one of them. */
export function selectedFields(configuration: RecordModifiedTriggerConfiguration): string[] {
  const system = new Set(TriggerSystemFields.map(name => name.toLowerCase()));
  return [...TriggerSystemFields, ...(configuration.fieldNames ?? []).filter(name => !system.has(name.toLowerCase()))];
}

/**
 * One created or modified record read by polling. A record changed several times between two polls arrives once,
 * in its latest state; a deleted record is not delivered. Field names match the Go connector.
 */
export interface RecordModifiedEvent {
  /** The watched object's API name. */ sObjectType: string;
  /** The record's 18-character ID. */ recordId: string;
  /** The derived change kind; see RecordChangeKind. */ changeKind: RecordChangeKind;
  /** The record's SystemModstamp in UTC, the time of the change, as Go encodes a time.Time. */ systemModstamp: string;
  /**
   * Id, CreatedDate, LastModifiedDate, SystemModstamp, and every configured field as Salesforce returned them, with
   * the field, object, and sharing access of the authorized user.
   */
  record: SalesforceRecord;
}

/** Decodes a recordModifiedPolled payload that this package or the Go connector recorded. */
export const RecordModifiedEventSchema: z.ZodType<RecordModifiedEvent> = z.object({
  sObjectType: z.string(),
  recordId: z.string(),
  changeKind: z.enum([RecordChangeCreated, RecordChangeUpdated]),
  systemModstamp: z.string().transform((value, context) => {
    const parsed = parseRFC3339(value);
    if (!parsed) { context.addIssue({ code: 'custom', message: 'systemModstamp must be an RFC 3339 time' }); return z.NEVER; }
    return formatGoTime(parsed);
  }),
  record: RecordSchema,
});

/** Manifest definition of recordModifiedPolled. */
export const RecordModifiedPolledTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'recordModifiedPolled' },
  description: 'Receive each record of one object that was created or modified after the Trigger first started, read by polling SOQL on SystemModstamp and Id; the first run records a baseline and delivers no existing records, and deletions are not delivered.',
};

/** One static application binding of recordModifiedPolled. */
export interface RecordModifiedPolledTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units, such as the sObjectPicker and fieldNamesInput units. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static recordModifiedPolled binding for Flow Definition tooling and Dex Web. */
export function defineRecordModifiedPolledTriggerBinding(config: RecordModifiedPolledTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: RecordModifiedPolledTriggerDefinition, ...config });
}
