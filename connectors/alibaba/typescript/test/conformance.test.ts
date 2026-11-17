// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { AlibabaClient } from '../src/client.js';
import { DescribeRegionsInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/alibaba/scenarios.json', import.meta.url));
const connection = { provider: 'alibaba', name: 'alibaba' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAlibaba));

async function invokeAlibaba(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'describeRegions') throw new Error(`unknown Alibaba Cloud operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentialEntries: Array<[typeof connection, Credentials]> = [[connection, {
      accessKeyId: new SecretString(scenario.name === 'describeRegions-defect' ? 'bad key' : 'test-access-key-id'),
      accessKeySecret: new SecretString('test-access-key-secret'),
    }]];
    const client = new AlibabaClient({
      baseUrl: 'http://127.0.0.1:12345',
      ...(scenario.name === 'describeRegions-oversized' ? { maxResponseBytes: 4 } : {}),
    }, new StaticCredentialProvider<Credentials>(credentialEntries), {
      clock: () => new Date('2023-10-26T10:22:32Z'),
      nonceSource: () => Buffer.from('3156853299f313e23dc12e1703dc12e1', 'hex'),
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}/`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'alibaba-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'alibaba', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.describeRegions().invoke(call, DescribeRegionsInputSchema.parse(scenario.input));
  });
}
