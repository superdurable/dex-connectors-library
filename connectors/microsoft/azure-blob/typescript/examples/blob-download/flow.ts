// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetBlobStep, type Connection, type BlobDownload, type GetBlobInput,
} from '@superdurable/connector-microsoft-azure-blob';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'microsoft-azure-blob';
const functionDetailsAttribute = new Attribute<BlobDownload>('azureblob-blob-download', jsonCodec<BlobDownload>());

/** Read one Azure blob through a typed Azure Blob Storage Query. */
export class BlobDownloadFlow implements Flow<GetBlobInput> {
  private readonly complete = new CompleteBlobDownload();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Azure Blob Storage connection name differs');
    this.read = newGetBlobStep<GetBlobInput>({
      stepType: 'CallGetBlob',
      annotations: { groupId: 'provider', groupLabel: 'Azure Blob Storage', explanation: 'Read one Azure blob from Azure Blob Storage.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      downloaded: new StepRef('CompleteGetBlob'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'MicrosoftAzureBlobDownloadTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetBlobInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected blob bytes. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [functionDetailsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the downloaded blob and complete the Flow."
class CompleteBlobDownload implements Step<QueryResult<BlobDownload>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGetBlob'; }
  /** Persist and return the downloaded blob. */
  execute(context: Context, result: QueryResult<BlobDownload>): StepDecision {
    functionDetailsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
