// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { QingcloudClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qingcloud/scenarios.json', import.meta.url));
const connection = { provider: 'qingcloud', name: 'qingcloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQingcloud, ''));
async function invokeQingcloud(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'describeZones') throw new Error(`unknown QingCloud operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { accessKeyId: scenario.name.endsWith('-defect') ? '' : 'REDACTED_QINGCLOUD_ACCESS_KEY', secretAccessKey: new SecretString('REDACTED_QINGCLOUD_SECRET') }]]);
  const client = new QingcloudClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-12T10:00:00Z') });
  const call: Call = { context: { flowId: 'qingcloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'qingcloud', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.describeZones().invoke(call, scenario.input as unknown as Parameters<ReturnType<QingcloudClient['describeZones']>['invoke']>[1]);
}
