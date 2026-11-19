// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { XiaoetechClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/xiaoetech/scenarios.json', import.meta.url));
const connection = { provider: 'xiaoetech', name: 'xiaoetech' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeXiaoetech, ''));

async function invokeXiaoetech(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listRootGoodsCategories') throw new Error(`unknown Xiaoetech operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appId: isDefect ? '' : 'app-test', clientId: 'xop-test', secretKey: new SecretString('REDACTED_SECRET_KEY') }]]);
  const client = new XiaoetechClient(scenario.name.endsWith('-oversized') ? {maxResponseBytes: 16} : {}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-12T10:00:00Z') });
  const call: Call = {
    context: { flowId: 'xiaoetech-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'xiaoetech', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listRootGoodsCategories().invoke(call);
}
