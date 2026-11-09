// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetSignatureRequestStep, type Connection, type SignatureRequestStatus, type GetSignatureRequestInput,
} from '@superdurable/connector-dropbox-sign';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'dropbox-sign';
const statusAttribute = new Attribute<SignatureRequestStatus>('dropbox-sign-signature-request', jsonCodec<SignatureRequestStatus>());

/** Read one signature request through a typed Dropbox Sign Query. */
export class SignatureRequestInspectionFlow implements Flow<GetSignatureRequestInput> {
  private readonly complete = new CompleteSignatureRequestInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Dropbox Sign connection name differs');
    this.read = newGetSignatureRequestStep<GetSignatureRequestInput>({
      stepType: 'CallGetSignatureRequest',
      annotations: { groupId: 'provider', groupLabel: 'Dropbox Sign', explanation: 'Read one signature request from Dropbox Sign.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteDropboxSign'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'DropboxSignSignatureRequestInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetSignatureRequestInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected signature request result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [statusAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected signature request and complete the Flow."
class CompleteSignatureRequestInspection implements Step<QueryResult<SignatureRequestStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteDropboxSign'; }
  /** Persist and return the selected signature request. */
  execute(context: Context, result: QueryResult<SignatureRequestStatus>): StepDecision {
    statusAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
