// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newDeleteDocumentStep, type Connection, type DeleteDocumentInput, type DeletedDocument } from '@superdurable/connector-elasticsearch';

export const ConnectionName = 'elasticsearch';
const resultAttribute = new Attribute<DeletedDocument>('elasticsearch-typescript-deleted-document', jsonCodec<DeletedDocument>());

/** Ensure one Elasticsearch document ID is absent. */
export class DocumentDeleteFlow implements Flow<DeleteDocumentInput> {
  private readonly complete = new CompleteElasticsearchDelete();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Elasticsearch connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'ElasticsearchTypeScriptDocumentDeleteFlow'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<DeleteDocumentInput> {
    return StepList.startStep(newDeleteDocumentStep<DeleteDocumentInput>({
      stepType: 'CallElasticsearchDeleteDocument',
      annotations: { groupId: 'document-delete', groupLabel: 'Document delete',
        explanation: 'Ensure one Elasticsearch document ID is absent.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, absent: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [resultAttribute] };
  }
}

// dex:group group-id:document-delete group-label:"Document delete"
// dex:explanation text:"Persist the confirmed Elasticsearch result and complete the Flow."
export class CompleteElasticsearchDelete implements Step<MutationResult<DeletedDocument>> {
  readonly inputCodec = jsonCodec<MutationResult<DeletedDocument>>();
  getStepType(): string { return 'CompleteElasticsearchDelete'; }
  execute(context: Context, result: MutationResult<DeletedDocument>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
