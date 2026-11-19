// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TaosdataClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/taosdata/scenarios.json', import.meta.url));
const connection = { provider: 'taosdata', name: 'taosdata' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTaosdata, ''));

async function invokeTaosdata(scenario: Scenario, recorder: ExchangeRecorder) {
  const cloudToken = scenario.name.endsWith('defect') ? '' : 'secret-cloud-token';
  const credentials: Array<[typeof connection, { cloudToken: SecretString; cloudUrl: SecretString }]> = [[connection, { cloudToken: new SecretString(cloudToken), cloudUrl: new SecretString('https://abc.cloud.tdengine.com') }]];
  const client = new TaosdataClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'taosdata-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'taosdata', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listDatabases') return client.listDatabases().invoke(call);
  throw new Error(`unknown Taosdata operation ${scenario.operation}`);
}
