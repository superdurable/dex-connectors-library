// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { YijingtongClient } from '../src/client.js';
import { ListWarehousesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/yijingtong/scenarios.json', import.meta.url));
const connection = { provider: 'yijingtong', name: 'yijingtong' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYijingtong));

async function invokeYijingtong(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listWarehouses') throw new Error(`unknown Yijingtong operation ${scenario.operation}`);
  const credentials = { token: new SecretString(scenario.name.endsWith('-defect') ? '' : 'REDACTED_YIJINGTONG_TOKEN') };
  const client = new YijingtongClient({ baseUrl: 'https://tenant.example', ...(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'yijingtong-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'yijingtong', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listWarehouses().invoke(call, ListWarehousesInputSchema.parse(scenario.input));
}
