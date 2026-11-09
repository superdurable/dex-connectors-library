// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import {
  OpenSearchClient, DeleteDocumentInputSchema, IndexDocumentInputSchema, SearchDocumentsInputSchema,
  type DeletedDocument, type IndexedDocument,
} from '../src/index.js';
import {
  answerRecordedRequest, invokeProductionMutation, loadScenarios, replay, scenarioCall,
  type ExchangeRecorder, type HeartbeatContext, type Scenario,
} from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/opensearch/scenarios.json', import.meta.url));
const connection = { provider: 'opensearch', name: 'opensearch' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOpenSearch));

async function invokeOpenSearch(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new OpenSearchClient({ clusterUrl: 'http://127.0.0.1:12345' },
    { resolve: async () => ({ username: 'dex_reader', password: new SecretString('fake-password') }) },
    { send: sender });
  const call = scenarioCall(scenario, connection, 'opensearch', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'indexDocument') {
    const input = IndexDocumentInputSchema.parse(scenario.input);
    return invokeProductionMutation(client.indexDocument(), scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<typeof input, IndexedDocument>, call, input),
      error => error instanceof RetryError);
  }
  if (scenario.operation === 'deleteDocument') {
    const input = DeleteDocumentInputSchema.parse(scenario.input);
    return invokeProductionMutation(client.deleteDocument(), scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<typeof input, DeletedDocument>, call, input),
      error => error instanceof RetryError);
  }
  if (scenario.operation === 'searchDocuments')
    return client.searchDocuments().invoke(call, SearchDocumentsInputSchema.parse(scenario.input));
  throw new Error(`unknown OpenSearch operation ${scenario.operation}`);
}
