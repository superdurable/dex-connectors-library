// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZillizClient } from '../src/client.js';
import { ListClustersInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/zilliz/scenarios.json', import.meta.url));
const connection = { provider: 'zilliz', name: 'zilliz' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZilliz));

async function invokeZilliz(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listClusters') throw new Error(`unknown Zilliz operation ${scenario.operation}`);
  const client = new ZillizClient(scenario.name === 'listClusters-oversized' ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name === 'listClusters-defect' ? '' : 'test-secret') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'zilliz-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'zilliz', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listClusters().invoke(call, ListClustersInputSchema.parse(scenario.input));
}
