// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newUpsertRowStep, type Connection, type UpsertRowInput, type UpsertRowOutput } from '@superdurable/connector-google-sheets';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'google-sheets-contacts';
/** Nonsecret row input retained for Flow inspection. */
export const contactRequest = new Attribute<UpsertRowInput>('google-sheets-contact-request', jsonCodec<UpsertRowInput>());
/** Confirmed row result retained for Flow inspection. */
export const contactResult = new Attribute<UpsertRowOutput>('google-sheets-contact-result', jsonCodec<UpsertRowOutput>());

/** Upsert one contact keyed by a stable column. */
export class UpsertContactFlow implements Flow<UpsertRowInput> {
  private readonly prepare = new PrepareContact();
  private readonly complete = new CompleteContact();
  /** Bind the named Google Sheets connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Google Sheets connection is required'); }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'GoogleSheetsUpsertContactFlow'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<UpsertRowInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newUpsertRowStep({
        stepType: 'UpsertGoogleSheetsContact',
        annotations: { groupId: 'google-sheets-contact', groupLabel: 'Google Sheets Contact', explanation: 'Find the stable contact key and replace or append one row.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        upserted: this.complete,
      }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [contactRequest, contactResult] }; }
}

/** Validate public row selection and retain the safe request. */
// dex:group group-id:google-sheets-contact group-label:"Google Sheets Contact"
// dex:explanation text:"Validate the selected spreadsheet and stable contact key before writing."
export class PrepareContact implements Step<UpsertRowInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareGoogleSheetsContact'; }
  /** Start the connector Step. */
  execute(context: Context, input: UpsertRowInput): StepDecision {
    if (!input.spreadsheetId?.trim() || !input.sheetName?.trim() || !input.keyColumn?.trim() || !input.keyValue?.trim() || !input.values || !Object.keys(input.values).length) return forceFail('A spreadsheet, tab, key, and values are required');
    contactRequest.set(context, input);
    return goTo(new StepRef('UpsertGoogleSheetsContact'), input);
  }
}

/** Persist the confirmed provider row and complete the Flow. */
// dex:group group-id:google-sheets-contact group-label:"Google Sheets Contact"
// dex:explanation text:"Persist the confirmed contact row and finish the Flow."
export class CompleteContact implements Step<MutationResult<UpsertRowOutput>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteGoogleSheetsContact'; }
  /** Return the confirmed row locator. */
  execute(context: Context, result: MutationResult<UpsertRowOutput>): StepDecision {
    contactResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
