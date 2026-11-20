// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WinitClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/winit/scenarios.json', import.meta.url));
const connection = { provider: 'winit', name: 'winit' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWinit, ''));

async function invokeWinit(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listWarehouses') throw new Error(`unknown Winit operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appKey: new SecretString(isDefect ? '' : 'REDACTED_APP_KEY'), token: new SecretString('REDACTED_TOKEN'), clientId: new SecretString('REDACTED_CLIENT_ID'), clientSecret: new SecretString('REDACTED_CLIENT_SECRET') }]]);
  const client = new WinitClient({ platform: 'OWNERERP', ...(scenario.name.endsWith('-oversized') ? {maxResponseBytes: 16} : {}) }, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-13T04:00:00Z') });
  const call: Call = {
    context: { flowId: 'winit-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'winit', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listWarehouses().invoke(call);
}
