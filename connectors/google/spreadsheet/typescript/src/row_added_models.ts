// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';

/** Google Picker UI unit that stores the chosen spreadsheet ID. */ export const UIUnitSpreadsheetPicker = 'spreadsheetPicker';
/** Spreadsheet picker port holding the spreadsheet ID; bind it to /spreadsheetId. */ export const UISpreadsheetPickerPortSpreadsheetID = 'spreadsheetId';
/** UI unit that lists one spreadsheet's tab titles and stores the exact chosen title. */ export const UIUnitSheetTabPicker = 'sheetTabPicker';
/** Tab picker input port naming the spreadsheet whose tabs it lists. */ export const UISheetTabPickerPortSpreadsheetID = 'spreadsheetId';
/** Tab picker port holding the exact tab title; bind it to /sheetName. */ export const UISheetTabPickerPortTab = 'tab';

/**
 * Selects the spreadsheet tab one rowAdded binding polls, as Dex Web saves it from the spreadsheetPicker and
 * sheetTabPicker units. Unknown fields are rejected and null counts as blank, as Go decodes it;
 * validateRowAddedTriggerConfiguration then requires both values.
 */
export const RowAddedTriggerConfigurationSchema = z.strictObject({
  /** The Google spreadsheet ID stored by the spreadsheetPicker unit. */ spreadsheetId: z.string().nullish().transform(value => value ?? ''),
  /**
   * The exact tab title stored by the sheetTabPicker unit, used verbatim, including leading or trailing spaces.
   * Row 1 of the tab is the header row whose cells name RowAddedEvent.values.
   */
  sheetName: z.string().nullish().transform(value => value ?? ''),
});
/** The spreadsheet ID and exact tab title of one rowAdded binding. */
export interface RowAddedTriggerConfiguration {
  /** The Google spreadsheet ID stored by the spreadsheetPicker unit. Required. */ spreadsheetId: string;
  /** The exact tab title stored by the sheetTabPicker unit, used verbatim. Required. */ sheetName: string;
}

/** One row appended below the last processed row of the polled tab; field names match the Go connector. */
export const RowAddedEventSchema = z.object({
  /** The polled spreadsheet ID. */ spreadsheetId: z.string(),
  /** The polled tab title. */ sheetName: z.string(),
  /** The 1-based sheet row number when the row was read; row 1 is the header. */ rowNumber: z.number().int(),
  /**
   * Maps each non-empty, unique header cell to this row's formatted cell, or "" when the row has no value in
   * that column. Columns whose header is empty or repeated appear only in cells.
   */
  values: z.record(z.string(), z.string()).nullable().transform(values => values ?? {}),
  /** The row's formatted cell values in column order, without trailing empty cells. */ cells: z.array(z.string()).nullable().transform(cells => cells ?? []),
});
/** One row appended below the last processed row of the polled tab. */
export interface RowAddedEvent {
  /** The polled spreadsheet ID. */ spreadsheetId: string;
  /** The polled tab title. */ sheetName: string;
  /** The 1-based sheet row number when the row was read; row 1 is the header. */ rowNumber: number;
  /** Header-keyed formatted cells of the row. */ values: Record<string, string>;
  /** The row's formatted cells in column order, without trailing empty cells. */ cells: string[];
}

/** Manifest definition of rowAdded. */
export const RowAddedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'rowAdded' },
  description: 'Receive each row appended below the last processed row of one picked spreadsheet tab.',
};

/** One static application binding of rowAdded. */
export interface RowAddedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio units for the binding, such as the spreadsheetPicker and sheetTabPicker units. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static rowAdded binding for Flow Definition tooling and Dex Web. */
export function defineRowAddedTriggerBinding(config: RowAddedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: RowAddedTriggerDefinition, ...config });
}

/**
 * Trim Go's unicode.IsSpace set, as strings.TrimSpace does: unlike String.prototype.trim it trims U+0085 and
 * keeps U+FEFF, so a trimmed spreadsheet ID, and the event IDs built from it, match the Go connector's.
 */
export function goTrimSpace(value: string): string {
  return value.replace(/^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g, '');
}

/**
 * Check that a binding names a spreadsheet and a tab; whitespace-only values count as blank. Throws a
 * TypeError otherwise.
 */
export function validateRowAddedTriggerConfiguration(configuration: RowAddedTriggerConfiguration): void {
  if (typeof configuration?.spreadsheetId !== 'string' || typeof configuration.sheetName !== 'string' || !goTrimSpace(configuration.spreadsheetId) || !goTrimSpace(configuration.sheetName)) throw new TypeError('Google Sheets rowAdded spreadsheetId and sheetName are required');
}
