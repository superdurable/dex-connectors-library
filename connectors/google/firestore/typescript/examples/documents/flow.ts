// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDocumentsStep, type Connection, type ListDocumentsInput, type DocumentsPage } from '@superdurable/connector-google-firestore';

/** Static Dex Web connection for the Documents example. */
export const ConnectionName = 'google-firestore';
const resultAttribute = new Attribute<DocumentsPage>('google-firestore-typescript-page', jsonCodec<DocumentsPage>());

/** List documents visible to the authorized Google identity. */
export class DocumentsFlow implements Flow<ListDocumentsInput> {
  private readonly complete = new CompleteDocuments();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Documents connection name differs');
    this.read = newListDocumentsStep<ListDocumentsInput>({
      stepType: 'CallListDocuments',
      annotations: { groupId: 'provider', groupLabel: 'Firestore', explanation: 'List documents visible in the selected Google Cloud project.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FirestoreDocumentsListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListDocumentsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the documents and complete the Flow."
class CompleteDocuments implements Step<QueryResult<DocumentsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<DocumentsPage>>();
  getStepType(): string { return 'CompleteDocuments'; }
  execute(context: Context, result: QueryResult<DocumentsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
