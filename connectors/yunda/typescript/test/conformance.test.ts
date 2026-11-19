// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { YundaClient } from '../src/client.js';
import { GetWaybillBalanceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/yunda/scenarios.json', import.meta.url));
const connection = { provider: 'yunda', name: 'yunda' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYunda));

async function invokeYunda(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getWaybillBalance') throw new Error(`unknown Yunda operation ${scenario.operation}`);
  const credentials = {
    appKey: new SecretString(scenario.name === 'getWaybillBalance-defect' ? '' : '999999'),
    appSecret: new SecretString('REDACTED_APP_SECRET'),
    partnerID: new SecretString('REDACTED_PARTNER_ID'),
    baimaSecret: new SecretString('REDACTED_BAIMA_SECRET'),
  };
  const client = new YundaClient(scenario.name === 'getWaybillBalance-oversized' ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, credentials]]), {
    now: () => 1731510000000,
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'yunda-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'yunda', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getWaybillBalance().invoke(call, GetWaybillBalanceInputSchema.parse(scenario.input));
}
