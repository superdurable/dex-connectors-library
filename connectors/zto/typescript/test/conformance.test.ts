// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZtoClient } from '../src/client.js';
import { QueryWaybillTraceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/zto/scenarios.json', import.meta.url));
const connection = { provider: 'zto', name: 'zto' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZto));

async function invokeZto(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'queryWaybillTrace') throw new Error(`unknown ZTO operation ${scenario.operation}`);
  const credentials = { appKey: scenario.name.endsWith('-defect') ? '' : 'app123', appSecret: new SecretString('secretKey') };
  const client = new ZtoClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'zto-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'zto', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.queryWaybillTrace().invoke(call, QueryWaybillTraceInputSchema.parse(scenario.input));
}
