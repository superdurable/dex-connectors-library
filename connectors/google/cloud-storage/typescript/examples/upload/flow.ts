// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newUploadObjectStep, type Connection, type UploadObjectInput, type Object } from '@superdurable/connector-google-cloud-storage';

/** Static Dex Web connection for the upload example. */
export const ConnectionName = 'google-cloud-storage';
const resultAttribute = new Attribute<Object>('google-cloud-storage-uploaded-typescript', jsonCodec<Object>());

/** Upload one object to the selected Cloud Storage bucket. */
export class UploadFlow implements Flow<UploadObjectInput> {
  private readonly complete = new CompleteUpload();
  private readonly upload;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Objects connection name differs');
    this.upload = newUploadObjectStep<UploadObjectInput>({
      stepType: 'CallUploadObject',
      annotations: { groupId: 'provider', groupLabel: 'Google Cloud Storage', explanation: 'Upload one object to the selected bucket.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, uploaded: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleCloudStorageUploadTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<UploadObjectInput> {
    return StepList.startStep(this.upload).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the uploaded object identity and complete the Flow."
class CompleteUpload implements Step<MutationResult<Object>> {
  readonly inputCodec = jsonCodec<MutationResult<Object>>();
  getStepType(): string { return 'CompleteUpload'; }
  execute(context: Context, result: MutationResult<Object>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
