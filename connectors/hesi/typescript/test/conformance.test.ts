// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { HesiClient } from '../src/client.js';
import { ListDepartmentsInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/hesi/scenarios.json', import.meta.url));
const connection = { provider: 'hesi', name: 'hesi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHesi));

async function invokeHesi(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listDepartments') throw new Error(`unknown Hesi operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { appKey: new SecretString('test-app-key'), appSecurity: new SecretString('test-app-security') };
    const client = new HesiClient({ ...(scenario.name === 'listDepartments-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        const original = new URL(request.url);
        return sendProviderRequest({ ...request, url: `${baseUrl}${original.pathname}${original.search}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'hesi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'hesi', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listDepartments().invoke(call, ListDepartmentsInputSchema.parse(scenario.input));
  });
}
