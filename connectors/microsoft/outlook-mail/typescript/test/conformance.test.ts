// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError,runMutation,SecretString,StaticCredentialProvider,type Call,type CredentialRefreshDriver,type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError,type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { OutlookMailClient } from '../src/client.js';
import { SearchMessagesInputSchema,GetMessageInputSchema,SendMessageInputSchema,ReplyToMessageInputSchema,MoveMessageInputSchema,SetMessageFlagsInputSchema,type Credentials } from '../src/generated/models.js';
import { answerRecordedRequest,ExchangeRecorder,HeartbeatContext,invokeProductionMutation,loadScenarios,replay,scenarioCall,type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios=loadScenarios(new URL('../../../../../conformance/connectors/microsoft/outlook-mail/scenarios.json',import.meta.url));
const connection={provider:'microsoft',name:'outlook-mail-test'};
const credentials:Credentials={authMethodId:'microsoft-oauth',clientId:'6731de76-14a6-49ae-8b2e-0ec0c2d8d5a1',clientSecret:new SecretString('test-client-secret-not-an-entra-value'),accessToken:new SecretString('OUTLOOK_CONFORMANCE_TOKEN'),refreshToken:new SecretString('initial-refresh-token')};
function productionMutation<Input,Output>(operation:Mutation<Input,Output>,call:Call,input:Input,key:string){return invokeProductionMutation(operation,key,recording=>runMutation(recording as Mutation<Input,Output>,call,input),error=>error instanceof RetryError);}
it.each(scenarios)('matches Go conformance: $name',scenario=>replay(scenario,invokeOutlookAttempt,'/v1.0'));
async function invokeOutlookAttempt(scenario:Scenario,recorder:ExchangeRecorder,context:HeartbeatContext){
  const driver:CredentialRefreshDriver<Credentials>={refreshRequired:()=>false,refresh:async()=>{throw new Error('unexpected credential refresh');}};
  const transport=(request:ProviderRequest)=>answerRecordedRequest(recorder,request,before=>new ProviderTransportError(before));
  const isAppOnly=scenario.name.includes('appOnly');
  const selectedCredentials:Credentials=isAppOnly?{...credentials,authMethodId:'app-only',tenantId:'contoso.onmicrosoft.com'}:credentials;
  const client=new OutlookMailClient(isAppOnly?{mailbox:'support@contoso.example'}:{},new StaticCredentialProvider([[connection,selectedCredentials]]),{refreshDriver:driver,transport,now:()=>new Date(scenario.fixedClock??'1970-01-01T00:00:01Z')});
  const call=scenarioCall(scenario,connection,'outlook-mail',context) as Call;
  switch(scenario.operation){
    case 'searchMessages':return client.searchMessages().invoke(call,SearchMessagesInputSchema.parse(scenario.input));
    case 'getMessage':return client.getMessage().invoke(call,GetMessageInputSchema.parse(scenario.input));
    case 'sendMessage':return productionMutation(client.sendMessage(),call,SendMessageInputSchema.parse(scenario.input),scenario.idempotencyKey!);
    case 'replyToMessage':return productionMutation(client.replyToMessage(),call,ReplyToMessageInputSchema.parse(scenario.input),scenario.idempotencyKey!);
    case 'moveMessage':return productionMutation(client.moveMessage(),call,MoveMessageInputSchema.parse(scenario.input),scenario.idempotencyKey!);
    case 'setMessageFlags':return productionMutation(client.setMessageFlags(),call,SetMessageFlagsInputSchema.parse(scenario.input),scenario.idempotencyKey!);
    default:throw new Error('unknown operation '+scenario.operation);
  }
}
