// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WapiClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/wapi/scenarios.json', import.meta.url));
const connection = { provider: 'wapi', name: 'wapi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWapi, ''));

async function invokeWapi(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listVehicleBrands') throw new Error(`unknown Wapi operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appID: isDefect ? '' : '123', appSecret: new SecretString('REDACTEDAPPSECRET000000000000000') }]]);
  const client = new WapiClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-19T12:00:00Z') });
  const call: Call = {
    context: { flowId: 'wapi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'wapi', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listVehicleBrands().invoke(call);
}
