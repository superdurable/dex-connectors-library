// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDatasetsStep, type Connection, type ListDatasetsInput, type DatasetsPage } from '@superdurable/connector-google-bigquery';

/** Static Dex Web connection for the Datasets example. */
export const ConnectionName = 'google-bigquery';
const resultAttribute = new Attribute<DatasetsPage>('google-bigquery-typescript-page', jsonCodec<DatasetsPage>());

/** List datasets visible to the authorized BigQuery customer. */
export class DatasetsFlow implements Flow<ListDatasetsInput> {
  private readonly complete = new CompleteDatasets();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Datasets connection name differs');
    this.read = newListDatasetsStep<ListDatasetsInput>({
      stepType: 'CallListDatasets',
      annotations: { groupId: 'provider', groupLabel: 'BigQuery', explanation: 'List datasets available to the authorized BigQuery customer.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'BigQueryDatasetsListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListDatasetsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the datasets and complete the Flow."
class CompleteDatasets implements Step<QueryResult<DatasetsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<DatasetsPage>>();
  getStepType(): string { return 'CompleteDatasets'; }
  execute(context: Context, result: QueryResult<DatasetsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
