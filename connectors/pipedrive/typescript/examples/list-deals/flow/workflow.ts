// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListObjectsStep, type Connection, type ListObjectsInput, type ObjectPage } from '@superdurable/connector-pipedrive';

/** Static Dex Web connection name for the Worker and starter. */
export const ConnectionName = 'pipedrive-crm';
/** Safe list request retained for Flow inspection. */
export const listRequest = new Attribute<ListObjectsInput>('pipedrive-list-request', jsonCodec<ListObjectsInput>());
/** Provider page retained for Flow inspection. */
export const dealPage = new Attribute<ObjectPage>('pipedrive-deal-page', jsonCodec<ObjectPage>());

/** List one bounded page of Pipedrive deals. */
export class ListDealsFlow implements Flow<ListObjectsInput> {
  private readonly prepare = new PrepareListDeals();
  private readonly complete = new CompleteListDeals();

  /** Bind one named connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Pipedrive connection is required'); }
  /** Stable Flow type. */ getFlowType(): string { return 'PipedriveListDealsFlow'; }
  /** Register the preparation, provider, and completion Steps. */
  getSteps(): StepList<ListObjectsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListObjectsStep({
        stepType: 'ListPipedriveDeals',
        annotations: { groupId: 'pipedrive-list', groupLabel: 'Pipedrive Deals', explanation: 'List one bounded page of Pipedrive deals.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }), this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [listRequest, dealPage] }; }
}

/** Validate and persist a safe request. */
// dex:group group-id:pipedrive-list group-label:"Pipedrive Deals"
// dex:explanation text:"Check the bounded list request before calling Pipedrive."
export class PrepareListDeals implements Step<ListObjectsInput> {
  /** Stable Step type. */ getStepType(): string { return 'PreparePipedriveListDeals'; }
  /** Start the provider Step with safe input. */
  execute(context: Context, input: ListObjectsInput): StepDecision {
    if (input.objectType !== 'deals') return forceFail('The example lists deals only');
    listRequest.set(context, input);
    return goTo(new StepRef('ListPipedriveDeals'), input);
  }
}

/** Complete the Flow with the returned page. */
// dex:group group-id:pipedrive-list group-label:"Pipedrive Deals"
// dex:explanation text:"Save the deal page and complete the Flow."
export class CompleteListDeals implements Step<QueryResult<ObjectPage>> {
  /** Stable Step type. */ getStepType(): string { return 'CompletePipedriveListDeals'; }
  /** Persist the safe provider output. */
  execute(context: Context, result: QueryResult<ObjectPage>): StepDecision {
    dealPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
