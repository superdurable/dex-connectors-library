// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { UCloudClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ucloud/scenarios.json', import.meta.url));
const connection = { provider: 'ucloud', name: 'ucloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeUCloud, ''));

async function invokeUCloud(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getRegion') throw new Error(`unknown UCloud operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { publicKey: new SecretString(isDefect ? '' : 'test-public-key'), privateKey: new SecretString('test-private-key') }]]);
  const client = new UCloudClient({ ...(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}) }, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'ucloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'ucloud', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getRegion().invoke(call, scenario.input as unknown as Parameters<ReturnType<UCloudClient['getRegion']>['invoke']>[1]);
}
