// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JisuAPIClient } from '../src/client.js';
import { ListProvincesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/jisuapi/scenarios.json', import.meta.url));
const connection = { provider: 'jisuapi', name: 'jisuapi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJisuAPI, ''));

async function invokeJisuAPI(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { appkey: SecretString }]> = [[connection, { appkey: new SecretString(scenario.name.endsWith('-defect') ? '' : 'test-secret') }]];
  const client = new JisuAPIClient({ maxResponseBytes: 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'jisuapi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'jisuapi', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listProvinces') return client.listProvinces().invoke(call, ListProvincesInputSchema.parse(scenario.input));
  throw new Error('unknown JisuAPI operation ' + scenario.operation);
}
