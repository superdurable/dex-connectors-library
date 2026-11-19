// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { EasyapiClient } from '../src/client.js';
import { QueryShopInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/easyapi/scenarios.json', import.meta.url));
const connection = { provider: 'easyapi', name: 'easyapi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeEasyapi, ''));

async function invokeEasyapi(scenario: Scenario, recorder: ExchangeRecorder) {
  const appKey = scenario.name.endsWith('missing-credentials') ? '' : 'test-key';
  const credentials: Array<[typeof connection, { appKey: SecretString; appSecret: SecretString }]> = [[connection, { appKey: new SecretString(appKey), appSecret: new SecretString('private-secret') }]];
  const client = new EasyapiClient({ baseUrl: 'https://api.easyapi.com', maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'easyapi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'easyapi', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'queryShop') return client.queryShop().invoke(call, QueryShopInputSchema.parse(scenario.input));
  throw new Error(`unknown Easyapi operation ${scenario.operation}`);
}
