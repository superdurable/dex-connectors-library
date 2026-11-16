// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import type { Record as AirtableRecord } from './generated/models.js';

/**
 * Selects the table and Last modified time field a recordChanged binding polls. Dex Web saves it from the
 * basePicker, tablePicker, and lastModifiedTimeFieldPicker units; the names are shown in Dex Web only, so a
 * renamed base, table, or field keeps working. Field names match the Go connector.
 */
export interface RecordChangedTriggerConfiguration {
  /** The base ID, such as appXXXXXXXXXXXXXX. Required. */ baseId: string;
  /** The base name the picker showed; informational. */ baseName?: string;
  /** The table ID, such as tblXXXXXXXXXXXXXX. Required. */ tableId: string;
  /** The table name the picker showed; informational. */ tableName?: string;
  /**
   * The field ID, such as fldXXXXXXXXXXXXXX, of a Last modified time field of the table whose value includes
   * a time of day. Required. The field's own setting decides which edits count as a change.
   */
  fieldId: string;
  /** The field name the picker showed; informational. */ fieldName?: string;
}

/**
 * One record created or changed after the last processed change of the polled table. Field names and values
 * match the Go connector's JSON, so Go, Python, and TypeScript replicas share one inbox.
 */
export interface RecordChangedEvent {
  /** The polled base ID. */ baseId: string;
  /** The polled table ID. */ tableId: string;
  /** The record's Last modified time value when the poll read it, as Go's RFC 3339 UTC text with up to nanoseconds. */ lastModifiedTime: string;
  /** Whether the record's createdTime is within one second of lastModifiedTime: created and not edited since. */ isCreatedRecord: boolean;
  /** The record as Airtable returned it, with fields keyed by field name. */ record: AirtableRecord;
}

const goString = z.string().nullish().transform(value => value ?? '');
const optionalGoString = z.string().nullish().transform(value => value || undefined);

/** Decodes a saved binding configuration strictly, as Go does: unknown fields are refused, null is blank. */
export const RecordChangedTriggerConfigurationSchema = z.strictObject({
  baseId: goString, baseName: optionalGoString, tableId: goString, tableName: optionalGoString, fieldId: goString, fieldName: optionalGoString,
}).transform((configuration): RecordChangedTriggerConfiguration => ({
  baseId: configuration.baseId,
  ...(configuration.baseName ? { baseName: configuration.baseName } : {}),
  tableId: configuration.tableId,
  ...(configuration.tableName ? { tableName: configuration.tableName } : {}),
  fieldId: configuration.fieldId,
  ...(configuration.fieldName ? { fieldName: configuration.fieldName } : {}),
}));

/** Decodes a stored RecordChangedEvent as Go's json.Unmarshal does: unknown fields are ignored. */
export const RecordChangedEventSchema = z.object({
  baseId: goString,
  tableId: goString,
  lastModifiedTime: z.string(),
  isCreatedRecord: z.boolean().nullish().transform(value => value ?? false),
  record: z.object({
    id: goString,
    createdTime: z.string(),
    fields: z.record(z.string(), z.unknown()).nullish().transform(value => value ?? null),
  }),
}).transform((event): RecordChangedEvent => ({
  baseId: event.baseId, tableId: event.tableId, lastModifiedTime: event.lastModifiedTime, isCreatedRecord: event.isCreatedRecord,
  record: { id: event.record.id, createdTime: event.record.createdTime, fields: event.record.fields },
}));
