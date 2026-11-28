// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetDocumentStep, type Connection, type GetDocumentInput, type RetrievedDocument } from '@superdurable/connector-opensearch';

export const ConnectionName = 'opensearch';
const resultAttribute = new Attribute<RetrievedDocument>('opensearch-typescript-retrieved-document', jsonCodec<RetrievedDocument>());

/** Read one OpenSearch document by index and ID. */
export class DocumentGetFlow implements Flow<GetDocumentInput> {
  private readonly complete = new CompleteOpenSearchGet();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('OpenSearch connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'OpenSearchTypeScriptDocumentGetFlow'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetDocumentInput> {
    return StepList.startStep(newGetDocumentStep<GetDocumentInput>({
      stepType: 'CallOpenSearchGetDocument',
      annotations: { groupId: 'document-get', groupLabel: 'Document retrieval',
        explanation: 'Read one OpenSearch document by index and ID.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [resultAttribute] };
  }
}

// dex:group group-id:document-get group-label:"Document retrieval"
// dex:explanation text:"Persist the confirmed OpenSearch result and complete the Flow."
export class CompleteOpenSearchGet implements Step<QueryResult<RetrievedDocument>> {
  readonly inputCodec = jsonCodec<QueryResult<RetrievedDocument>>();
  getStepType(): string { return 'CompleteOpenSearchGet'; }
  execute(context: Context, result: QueryResult<RetrievedDocument>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
