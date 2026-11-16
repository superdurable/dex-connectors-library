// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { PageSchema, type Page } from './generated/models.js';

/** The derived kinds of one databaseRowChanged delivery, the values of DatabaseRowChangedEvent.changeKind. */
export const DatabaseRowChangeKind = {
  /**
   * A row whose last edited time equals its created time: Notion reports no change type, and a row edited within
   * the same minute it was created still reads as created.
   */
  Created: 'created',
  /** Any other edit to a row's properties or content. */ Updated: 'updated',
} as const;
/** One of the two DatabaseRowChangeKind values. */
export type DatabaseRowChangeKind = (typeof DatabaseRowChangeKind)[keyof typeof DatabaseRowChangeKind];

/**
 * One row created or edited in the watched data source; field names match the Go connector, so Go, Python, and
 * TypeScript replicas share one inbox. Edits of one row that Notion reports with the same last edited time
 * arrive once, and a row moved to the trash is not delivered.
 */
export interface DatabaseRowChangedEvent {
  /** The watched data source. */ dataSourceId: string;
  /** The derived change kind; see DatabaseRowChangeKind. */ changeKind: string;
  /** The row with its property values as the data source query returned it. */ row: Page;
}

/**
 * The databaseRowChanged binding configuration that the dataSourcePicker unit saves in Dex Web. Changing the
 * data source of a binding that already polled makes its next poll record a new baseline for the new data source.
 */
export interface DatabaseRowChangedTriggerConfiguration {
  /**
   * The watched data source, a Notion ID such as 2a4d6f80-1b3c-4d5e-8f70-112233445566 or a Notion URL ending in
   * one; the picker resolves it from a database link. It is required.
   */
  dataSourceId?: string | undefined;
  /** The picked data source's name, kept for display only. */ dataSourceName?: string | undefined;
}

/** Decodes a stored DatabaseRowChangedEvent written by Go, Python, or TypeScript. */
export const DatabaseRowChangedEventSchema = z.object({
  dataSourceId: z.string(),
  changeKind: z.string(),
  row: PageSchema,
}) satisfies z.ZodType<DatabaseRowChangedEvent>;

/** Decodes a saved binding configuration strictly, as the Go connector does: unknown fields are refused. */
export const DatabaseRowChangedTriggerConfigurationSchema = z.strictObject({
  dataSourceId: z.string().optional(),
  dataSourceName: z.string().optional(),
}) satisfies z.ZodType<DatabaseRowChangedTriggerConfiguration>;
