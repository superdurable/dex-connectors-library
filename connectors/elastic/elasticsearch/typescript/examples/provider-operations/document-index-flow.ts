// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newIndexDocumentStep, type Connection, type IndexDocumentInput, type IndexedDocument } from '@superdurable/connector-elasticsearch';

export const ConnectionName = 'elasticsearch';
const resultAttribute = new Attribute<IndexedDocument>('elasticsearch-typescript-indexed-document', jsonCodec<IndexedDocument>());

/** Replace one Elasticsearch document under a stable ID. */
export class DocumentIndexFlow implements Flow<IndexDocumentInput> {
  private readonly complete = new CompleteElasticsearchIndex();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Elasticsearch connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'ElasticsearchTypeScriptDocumentIndexFlow'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<IndexDocumentInput> {
    return StepList.startStep(newIndexDocumentStep<IndexDocumentInput>({
      stepType: 'CallElasticsearchIndexDocument',
      annotations: { groupId: 'document-index', groupLabel: 'Document index',
        explanation: 'Replace one Elasticsearch document under a stable ID.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, indexed: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [resultAttribute] };
  }
}

// dex:group group-id:document-index group-label:"Document index"
// dex:explanation text:"Persist the confirmed Elasticsearch result and complete the Flow."
export class CompleteElasticsearchIndex implements Step<MutationResult<IndexedDocument>> {
  readonly inputCodec = jsonCodec<MutationResult<IndexedDocument>>();
  getStepType(): string { return 'CompleteElasticsearchIndex'; }
  execute(context: Context, result: MutationResult<IndexedDocument>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
