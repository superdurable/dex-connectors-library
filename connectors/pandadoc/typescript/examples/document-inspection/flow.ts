// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetDocumentStep, type Connection, type DocumentStatus, type GetDocumentInput,
} from '@superdurable/connector-pandadoc';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'pandadoc';
const documentAttribute = new Attribute<DocumentStatus>('pandadoc-document', jsonCodec<DocumentStatus>());

/** Read one document through a typed PandaDoc Query. */
export class DocumentInspectionFlow implements Flow<GetDocumentInput> {
  private readonly complete = new CompleteDocumentInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('PandaDoc connection name differs');
    this.read = newGetDocumentStep<GetDocumentInput>({
      stepType: 'CallGetDocument',
      annotations: { groupId: 'provider', groupLabel: 'PandaDoc', explanation: 'Read one document from PandaDoc.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompletePandaDoc'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'PandaDocDocumentInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetDocumentInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected document result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [documentAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected document and complete the Flow."
class CompleteDocumentInspection implements Step<QueryResult<DocumentStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePandaDoc'; }
  /** Persist and return the selected document. */
  execute(context: Context, result: QueryResult<DocumentStatus>): StepDecision {
    documentAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
