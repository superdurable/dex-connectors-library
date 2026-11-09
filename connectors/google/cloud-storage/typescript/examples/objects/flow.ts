// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListObjectsStep, type Connection, type ListObjectsInput, type ObjectsPage } from '@superdurable/connector-google-cloud-storage';

/** Static Dex Web connection for the Objects example. */
export const ConnectionName = 'google-cloud-storage';
const resultAttribute = new Attribute<ObjectsPage>('google-cloud-storage-typescript-page', jsonCodec<ObjectsPage>());

/** List objects visible to the connected Google user. */
export class ObjectsFlow implements Flow<ListObjectsInput> {
  private readonly complete = new CompleteObjects();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Objects connection name differs');
    this.read = newListObjectsStep<ListObjectsInput>({
      stepType: 'CallListObjects',
      annotations: { groupId: 'provider', groupLabel: 'Google Cloud Storage', explanation: 'List objects available to the connected Google user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleCloudStorageListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListObjectsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the objects and complete the Flow."
class CompleteObjects implements Step<QueryResult<ObjectsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<ObjectsPage>>();
  getStepType(): string { return 'CompleteObjects'; }
  execute(context: Context, result: QueryResult<ObjectsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
