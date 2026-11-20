// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { StoClient } from '../src/client.js';
import { QueryWaybillTraceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/sto/scenarios.json', import.meta.url));
const connection = { provider: 'sto', name: 'sto' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSto));

async function invokeSto(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'queryWaybillTrace') throw new Error(`unknown STO operation ${scenario.operation}`);
  const credentials = { appKey: scenario.name.endsWith('-defect') ? '' : 'app123', appSecret: new SecretString('REDACTED_STO_APP_SECRET'), fromCode: 'resource123' };
  const client = new StoClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'sto-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'sto', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.queryWaybillTrace().invoke(call, QueryWaybillTraceInputSchema.parse(scenario.input));
}
