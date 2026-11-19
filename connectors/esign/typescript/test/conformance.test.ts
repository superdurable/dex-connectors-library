// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { EsignClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/esign/scenarios.json', import.meta.url));
const connection = { provider: 'esign', name: 'esign' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeEsign, ''));

async function invokeEsign(scenario: Scenario, recorder: ExchangeRecorder) {
  const appSecret = scenario.name.endsWith('missing-secret') ? '' : 'test-secret';
  const credentials: Array<[typeof connection, { appId: string; appSecret: SecretString }]> = [[connection, { appId: 'app-123', appSecret: new SecretString(appSecret) }]];
  const client = new EsignClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date(1794830400000),
  });
  const call: Call = {
    context: { flowId: 'esign-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'esign', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getSignFlowStatus') return client.getSignFlowStatus().invoke(call, scenario.input as { signFlowId: string });
  throw new Error(`unknown Esign operation ${scenario.operation}`);
}
