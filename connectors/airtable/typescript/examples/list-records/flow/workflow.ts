// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListRecordsStep, type Connection, type ListRecordsInput, type RecordPage } from '@superdurable/connector-airtable';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'airtable-policies';

/** Safe request fields retained for flow inspection. */
export const listRequest = new Attribute<ListRecordsInput>('airtable-list-request', jsonCodec<ListRecordsInput>());
/** Safe provider result retained for flow inspection. */
export const recordPage = new Attribute<RecordPage>('airtable-record-page', jsonCodec<RecordPage>());

/** One bounded Airtable record-list Flow. */
export class ListRecordsFlow implements Flow<ListRecordsInput> {
  private readonly prepare = new PrepareListRecords();
  private readonly complete = new CompleteListRecords();

  /** Bind a named Airtable connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Airtable connection is required');
  }

  /** Stable Dex Flow type. */
  getFlowType(): string { return 'AirtableListRecordsFlow'; }

  /** Register the preparation, provider, and completion Steps. */
  getSteps(): StepList<ListRecordsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListRecordsStep({
        stepType: 'ListAirtableRecords',
        annotations: { groupId: 'airtable-list', groupLabel: 'Airtable Records', explanation: 'List one bounded page of Airtable records.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }

  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [listRequest, recordPage] }; }
}

/** Validate public IDs and retain the safe request. */
// dex:group group-id:airtable-list group-label:"Airtable Records"
// dex:explanation text:"Validate the base and table selection before listing records."
export class PrepareListRecords implements Step<ListRecordsInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareAirtableListRecords'; }
  /** Start the provider Step with nonsecret input. */
  execute(context: Context, input: ListRecordsInput): StepDecision {
    if (!/^app[0-9A-Za-z]{14}$/.test(input.baseId) || !input.tableIdOrName?.trim()) return forceFail('Airtable baseId and tableIdOrName are required');
    listRequest.set(context, input);
    return goTo(new StepRef('ListAirtableRecords'), input);
  }
}

/** Persist the listed page and complete the Flow. */
// dex:group group-id:airtable-list group-label:"Airtable Records"
// dex:explanation text:"Persist the returned record page and complete the Flow."
export class CompleteListRecords implements Step<QueryResult<RecordPage>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteAirtableListRecords'; }
  /** Retain only the provider's safe result fields. */
  execute(context: Context, result: QueryResult<RecordPage>): StepDecision {
    recordPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
