// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { LingxingClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/lingxing/scenarios.json', import.meta.url));
const connection = { provider: 'lingxing', name: 'lingxing' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeLingxing, ''));

async function invokeLingxing(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listStores') throw new Error(`unknown Lingxing operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appId: new SecretString(isDefect ? '' : 'REDACTED-APP-ID1'), appSecret: new SecretString('REDACTED_APP_SECRET') }]]);
  const client = new LingxingClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-12T10:00:00Z') });
  const call: Call = {
    context: { flowId: 'lingxing-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'lingxing', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listStores().invoke(call);
}
