// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WeilingClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/weiling/scenarios.json', import.meta.url));
const connection = { provider: 'weiling', name: 'weiling' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWeiling, ''));

async function invokeWeiling(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listDepartments') throw new Error(`unknown Weiling operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appId: new SecretString(isDefect ? '' : 'REDACTED_APP_ID'), appSecret: new SecretString('REDACTED_APP_SECRET') }]]);
  const client = new WeilingClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-12T10:00:00Z') });
  const call: Call = {
    context: { flowId: 'weiling-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'weiling', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listDepartments().invoke(call);
}
