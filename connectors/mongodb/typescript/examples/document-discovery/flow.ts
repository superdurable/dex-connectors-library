// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newFindDocumentsStep, type Connection, type DocumentSet, type FindDocumentsInput } from '@superdurable/connector-mongodb';

/** Static Dex Web connection for the document discovery example. */
export const ConnectionName = 'mongodb';
const documentsAttribute = new Attribute<DocumentSet>('mongodb-typescript-documents', jsonCodec<DocumentSet>());

/** Find matching documents and retain the bounded result in an Attribute. */
export class DocumentDiscoveryFlow implements Flow<FindDocumentsInput> {
  private readonly complete = new CompleteDocuments();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('MongoDB connection name differs');
    this.read = newFindDocumentsStep<FindDocumentsInput>({
      stepType: 'CallFindDocuments',
      annotations: { groupId: 'provider', groupLabel: 'MongoDB', explanation: 'Find documents in the selected MongoDB collection.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'MongoDBTypeScriptDocumentDiscovery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<FindDocumentsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [documentsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the document set and complete the Flow."
class CompleteDocuments implements Step<QueryResult<DocumentSet>> {
  readonly inputCodec = jsonCodec<QueryResult<DocumentSet>>();
  getStepType(): string { return 'CompleteFindDocuments'; }
  execute(context: Context, result: QueryResult<DocumentSet>): StepDecision {
    documentsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
