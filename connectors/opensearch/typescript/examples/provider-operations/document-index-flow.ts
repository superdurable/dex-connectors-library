// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newIndexDocumentStep, type Connection, type IndexDocumentInput, type IndexedDocument } from '@superdurable/connector-opensearch';

export const ConnectionName = 'opensearch';
const resultAttribute = new Attribute<IndexedDocument>('opensearch-typescript-indexed-document', jsonCodec<IndexedDocument>());

/** Replace one OpenSearch document under a stable ID. */
export class DocumentIndexFlow implements Flow<IndexDocumentInput> {
  private readonly complete = new CompleteOpenSearchIndex();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('OpenSearch connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'OpenSearchTypeScriptDocumentIndexFlow'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<IndexDocumentInput> {
    return StepList.startStep(newIndexDocumentStep<IndexDocumentInput>({
      stepType: 'CallOpenSearchIndexDocument',
      annotations: { groupId: 'document-index', groupLabel: 'Document index',
        explanation: 'Replace one OpenSearch document under a stable ID.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, indexed: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [resultAttribute] };
  }
}

// dex:group group-id:document-index group-label:"Document index"
// dex:explanation text:"Persist the confirmed OpenSearch result and complete the Flow."
export class CompleteOpenSearchIndex implements Step<MutationResult<IndexedDocument>> {
  readonly inputCodec = jsonCodec<MutationResult<IndexedDocument>>();
  getStepType(): string { return 'CompleteOpenSearchIndex'; }
  execute(context: Context, result: MutationResult<IndexedDocument>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
