// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { AqaraClient } from '../src/client.js';
import { ListDevicesInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/aqara/scenarios.json', import.meta.url));
const connection = { provider: 'aqara', name: 'aqara' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAqara));

async function invokeAqara(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listDevices') throw new Error(`unknown Aqara operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { appId: 'app-id', keyId: 'key-id', appKey: new SecretString('app-key'), accessToken: new SecretString('access-token') };
    const client = new AqaraClient({ baseUrl, ...(scenario.name === 'listDevices-oversized' ? { maxResponseBytes: 4 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      clock: () => new Date('2023-10-26T10:22:32Z'),
      nonceSource: () => Buffer.from('3156853299f313e23dc12e1703dc12e1', 'hex'),
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic pre-dispatch transport failure');
        }
        return sendProviderRequest(request);
      },
    });
    const call: Call = {
      context: { flowId: 'aqara-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'aqara', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listDevices().invoke(call, ListDevicesInputSchema.parse(scenario.input));
  });
}
