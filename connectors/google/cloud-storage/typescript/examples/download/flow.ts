// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newDownloadObjectStep, type Connection, type DownloadObjectInput, type DownloadedObject } from '@superdurable/connector-google-cloud-storage';

/** Static Dex Web connection for the Download example. */
export const ConnectionName = 'google-cloud-storage';
const resultAttribute = new Attribute<DownloadedObject>('google-cloud-storage-typescript-download', jsonCodec<DownloadedObject>());

/** Download one bounded object from Google Cloud Storage. */
export class DownloadFlow implements Flow<DownloadObjectInput> {
  private readonly complete = new CompleteDownload();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Download connection name differs');
    this.read = newDownloadObjectStep<DownloadObjectInput>({
      stepType: 'CallDownloadObject',
      annotations: { groupId: 'provider', groupLabel: 'Google Cloud Storage', explanation: 'Download the selected object from the connected bucket.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, downloaded: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleCloudStorageDownloadTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<DownloadObjectInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the download and complete the Flow."
class CompleteDownload implements Step<QueryResult<DownloadedObject>> {
  readonly inputCodec = jsonCodec<QueryResult<DownloadedObject>>();
  getStepType(): string { return 'CompleteDownload'; }
  execute(context: Context, result: QueryResult<DownloadedObject>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
