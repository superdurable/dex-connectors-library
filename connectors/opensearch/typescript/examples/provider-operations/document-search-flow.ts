// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchDocumentsStep, type Connection, type SearchDocumentsInput, type SearchPage } from '@superdurable/connector-opensearch';

export const ConnectionName = 'opensearch';
const resultAttribute = new Attribute<SearchPage>('opensearch-typescript-search-page', jsonCodec<SearchPage>());

/** Search one OpenSearch index with a bounded page. */
export class DocumentSearchFlow implements Flow<SearchDocumentsInput> {
  private readonly complete = new CompleteOpenSearchSearch();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('OpenSearch connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'OpenSearchTypeScriptDocumentSearchFlow'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<SearchDocumentsInput> {
    return StepList.startStep(newSearchDocumentsStep<SearchDocumentsInput>({
      stepType: 'CallOpenSearchSearchDocuments',
      annotations: { groupId: 'document-search', groupLabel: 'Document search',
        explanation: 'Search one OpenSearch index with a bounded page.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [resultAttribute] };
  }
}

// dex:group group-id:document-search group-label:"Document search"
// dex:explanation text:"Persist the confirmed OpenSearch result and complete the Flow."
export class CompleteOpenSearchSearch implements Step<QueryResult<SearchPage>> {
  readonly inputCodec = jsonCodec<QueryResult<SearchPage>>();
  getStepType(): string { return 'CompleteOpenSearchSearch'; }
  execute(context: Context, result: QueryResult<SearchPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
