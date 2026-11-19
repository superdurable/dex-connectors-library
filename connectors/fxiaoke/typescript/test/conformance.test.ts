// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FxiaokeClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/fxiaoke/scenarios.json', import.meta.url));
const connection = { provider: 'fxiaoke', name: 'fxiaoke' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFxiaoke, ''));

async function invokeFxiaoke(scenario: Scenario, recorder: ExchangeRecorder) {
  const appId = scenario.name.endsWith('defect') ? '' : 'FSAID_123';
  const credentials = new StaticCredentialProvider([[connection, { appId, appSecret: new SecretString('app-secret'), permanentCode: new SecretString('permanent-code') }]]);
  const traces = scenario.exchanges.map(exchange => exchange.query?.thirdTraceId?.[0] ?? '');
  const client = new FxiaokeClient({ employeeId: 'employee-123', maxResponseBytes: scenario.name.endsWith('oversized') ? 16 : 1_048_576 }, credentials, {
    traceId: () => traces.shift() ?? '',
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'fxiaoke-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'fxiaoke', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listObjectMetadata') return client.listObjectMetadata().invoke(call);
  throw new Error('unknown Fxiaoke operation ' + scenario.operation);
}
