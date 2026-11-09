// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListDocumentsStep, type Connection, type DocumentsPage, type ListDocumentsInput,
} from '@superdurable/connector-lucid';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'lucid';
const documentsAttribute = new Attribute<DocumentsPage>('lucid-documents', jsonCodec<DocumentsPage>());

/** Read one document page through a typed Lucid Query. */
export class DocumentDiscoveryFlow implements Flow<ListDocumentsInput> {
  private readonly complete = new CompleteDocumentDiscovery();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Lucid connection name differs');
    this.read = newListDocumentsStep<ListDocumentsInput>({
      stepType: 'CallListDocuments',
      annotations: { groupId: 'provider', groupLabel: 'Lucid', explanation: 'Read one document page from Lucid.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteLucid'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'LucidDocumentDiscoveryTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListDocumentsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected document page result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [documentsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected document page and complete the Flow."
class CompleteDocumentDiscovery implements Step<QueryResult<DocumentsPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteLucid'; }
  /** Persist and return the selected document page. */
  execute(context: Context, result: QueryResult<DocumentsPage>): StepDecision {
    documentsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
