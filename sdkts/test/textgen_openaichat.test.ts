// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, expect, it } from 'vitest';
import { createServer, type Server } from 'node:http';
import { newWireFormat, type Profile } from '../src/textgen/openaichat/index.js';
import { newTextGenerationQuery } from '../src/textgen/query.js';
import { textGenerationBranchDefinitions, type TextGenerationRequest } from '../src/textgen/contract.js';
import { createCall } from '../src/credentials.js';
import type { AsyncContext } from '@superdurable/dex';

const request = (): TextGenerationRequest => ({messages:[{role:'user',text:'hi'}]});
const call = (deltas: string[] = []) => { const base = createCall({flowId:'chat-flow',stepExecutionId:'chat-step',attempt:1,recordHeartbeat:async () => {}} as unknown as AsyncContext,{provider:'chat',name:'test'},{connectorId:'chat-lab',operationId:'generateText'}); return {context:base.context,id:base.id,connection:base.connection,operation:base.operation,reportProgress:base.reportProgress,hasProgressStream:()=>false,hasTextStream:()=>true,writeText:(value:string)=>{deltas.push(value);}}; };
it.each([
  ['defaults send no sampling fields',{},request(),{model:'chat-model',messages:[{role:'user',content:'hi'}]}],
  ['system instructions, max_tokens, temperature, and effort',{reasoningEfforts:{xhigh:'very_high'}},{...request(),instructions:'Be brief.',maxOutputTokens:64,temperature:0.7,reasoningEffort:'xhigh'},{model:'chat-model',messages:[{role:'system',content:'Be brief.'},{role:'user',content:'hi'}],max_tokens:64,temperature:0.7,reasoning_effort:'very_high'}],
  ['developer instructions and max_completion_tokens',{instructionsRole:'developer',maxTokensField:'max_completion_tokens'},{...request(),instructions:'Be brief.',maxOutputTokens:64},{model:'chat-model',messages:[{role:'developer',content:'Be brief.'},{role:'user',content:'hi'}],max_completion_tokens:64}],
  ['streaming with usage',{streaming:'always',shouldRequestStreamUsage:true},request(),{model:'chat-model',messages:[{role:'user',content:'hi'}],stream:true,stream_options:{include_usage:true}}],
  ['first matching model rule applies',{modelRules:[{modelIDPattern:'^chat-(model|other)$',maxTokensField:'max_completion_tokens',streaming:'always'},{modelIDPrefix:'chat-',maxTokensField:'max_tokens'}]},{...request(),maxOutputTokens:8},{model:'chat-model',messages:[{role:'user',content:'hi'}],max_completion_tokens:8,stream:true}],
] as const)('Go Chat request body: %s',(_name,settings,input,expected) => {
  const format = newWireFormat({providerName:'chat-lab',...settings} as Profile);
  const encoded = format.encodeRequest({request:{...input,model:'chat-model'} as TextGenerationRequest,rules:format.rulesForModel('chat-model')});
  expect(JSON.parse(Buffer.from(encoded.body).toString())).toEqual(expected);
});
it.each([
  ['no provider',{}],['bad path',{providerName:'chat-lab',chatCompletionsPath:'https://example.com'}],['bad role',{providerName:'chat-lab',instructionsRole:'user'}],
  ['reserved header',{providerName:'chat-lab',fixedHeaders:{Authorization:'not-safe'}}],['missing stream field',{providerName:'chat-lab',streaming:'always',allowedRequestFields:['model','messages']}],
  ['unanchored model rule',{providerName:'chat-lab',modelRules:[{modelIDPattern:'chat-.*'}]}],
  ['fixed header value',{providerName:'chat-lab',fixedHeaders:{'X-Version':'1\n2'}}],
  ['invalid token field',{providerName:'chat-lab',maxTokensField:'max_output_tokens'}],
  ['invalid streaming policy',{providerName:'chat-lab',streaming:'sometimes'}],
  ['invalid effort wire value',{providerName:'chat-lab',reasoningEfforts:{low:'Low Effort'}}],
  ['unknown effort',{providerName:'chat-lab',reasoningEfforts:{ultra:'ultra'}}],
  ['invalid structured mode',{providerName:'chat-lab',structuredOutput:{mode:'xml'}}],
  ['invalid request field',{providerName:'chat-lab',allowedRequestFields:['Model']}],
  ['allowlist without messages',{providerName:'chat-lab',allowedRequestFields:['model','max_tokens']}],
  ['allowlist without stream options',{providerName:'chat-lab',streaming:'always',shouldRequestStreamUsage:true,allowedRequestFields:['model','messages','stream']}],
  ['rule with both matchers',{providerName:'chat-lab',modelRules:[{modelIDPrefix:'a',modelIDPattern:'^a$'}]}],
  ['rule without matcher',{providerName:'chat-lab',modelRules:[{}]}],
  ['invalid rule pattern',{providerName:'chat-lab',modelRules:[{modelIDPattern:'^(a$'}]}],
  ['invalid rule token field',{providerName:'chat-lab',modelRules:[{modelIDPrefix:'a',maxTokensField:'tokens'}]}],
] as const)('rejects invalid profile: %s',(_name,profile) => expect(() => newWireFormat(profile as Profile)).toThrow());
it('decodes content parts, reasoning, refusal, and usage', () => {
  const format = newWireFormat({providerName:'chat-lab'});
  const response = format.decodeResponse?.(Buffer.from(JSON.stringify({id:'r',model:'served',choices:[{index:0,message:{content:[{type:'thinking',text:'private'},{type:'text',text:'visible'}],reasoning_content:'hidden'},finish_reason:'stop'}],usage:{prompt_tokens:4,completion_tokens:3,total_tokens:7,prompt_tokens_details:{cached_tokens:1},completion_tokens_details:{reasoning_tokens:2}}})));
  expect(response?.parts).toEqual([{text:'hidden',isReasoning:true},{text:'visible'}]);
  expect(response?.usage).toEqual({inputTokens:4,outputTokens:3,totalTokens:7,cachedInputTokens:1,reasoningTokens:2});
});
it('treats Go null choice and content fields as zero values', () => {
  const format = newWireFormat({providerName:'chat-lab'});
  const response = format.decodeResponse?.(Buffer.from('{"choices":[{"index":null,"message":{"content":[{"type":"text","text":null},{"type":null,"text":"ignored"},{"type":"text","text":"ok"}]},"finish_reason":"stop"}]}'));
  expect(response?.parts).toEqual([{text:'ok'}]);
  expect(response?.providerFinishReason).toBe('stop');
});
it.each([
  ['text content',{content:'visible'},[{text:'visible'}]],
  ['null content',{content:null},[]],
  ['typed parts',{content:[null,{type:'image',text:'hidden'},{type:'text',text:'visible'}]},[{text:'visible'}]],
  ['reasoning then text',{reasoning_content:'thinking',content:'visible'},[{text:'thinking',isReasoning:true},{text:'visible'}]],
])('decodes Go Chat provider variant %s', (_name,message,parts) => {
  const format = newWireFormat({providerName:'chat-lab'});
  const response = format.decodeResponse?.(Buffer.from(JSON.stringify({choices:[{index:0,message,finish_reason:'stop'}]})));
  expect(response?.parts).toEqual(parts);
});
it.each([
  ['choices object',{choices:{}}],
  ['numeric model',{model:5,choices:[]}],
  ['numeric finish reason',{choices:[{index:0,finish_reason:5}]}],
  ['string token count',{choices:[{index:0,finish_reason:'stop'}],usage:{prompt_tokens:'3'}}],
  ['string usage details',{choices:[{index:0,finish_reason:'stop'}],usage:{prompt_tokens_details:'bad'}}],
])('rejects Go-invalid non-stream response field %s', (_name,body) => {
  const format = newWireFormat({providerName:'chat-lab'});
  expect(() => format.decodeResponse?.(Buffer.from(JSON.stringify(body)))).toThrow();
});
it.each([
  ['choices object',{choices:{}}],
  ['numeric model',{model:5,choices:[]}],
  ['numeric delta',{choices:[{index:0,delta:5}]}],
  ['numeric finish reason',{choices:[{index:0,finish_reason:5}]}],
  ['fractional index',{choices:[{index:0.5,finish_reason:'stop'}]}],
])('rejects Go-invalid stream response field %s', (_name,body) => {
  const format = newWireFormat({providerName:'chat-lab',streaming:'always'});
  expect(() => format.decodeStreamEvent?.({data:JSON.stringify(body)}, {response:{parts:[]},hasFinished:false,isDone:false}, () => {})).toThrow();
});
let server: Server | undefined;
afterEach(async () => { if (server) { await new Promise<void>(resolve => server?.close(() => resolve())); server = undefined; } });
it('streams text deltas before the terminal event and returns the whole result', async () => {
  const received: string[] = [];
  server = createServer(async (incoming,response) => {
    for await (const chunk of incoming) { void chunk; }
    response.setHeader('Content-Type','text/event-stream');
    response.write('data: '+JSON.stringify({id:'r',model:'served',choices:[{index:0,delta:{content:'Hel'},finish_reason:null}]})+'\n\n');
    await new Promise<void>(resolve => setImmediate(resolve));
    response.write('data: '+JSON.stringify({choices:[{index:0,delta:{content:'lo'},finish_reason:'stop'}],usage:{prompt_tokens:2,completion_tokens:1}})+'\n\n');
    response.end('data: [DONE]\n\n');
  });
  await new Promise<void>(resolve => server?.listen(0,'127.0.0.1',resolve));
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('missing address');
  const query = newTextGenerationQuery({definition:{operation:{connectorId:'chat-lab',operationId:'generateText'},branches:textGenerationBranchDefinitions(),stepDefaults:{executeDurability:'sync',executeMethodTimeoutMs:60000}},wireFormat:newWireFormat({providerName:'chat-lab',streaming:'always'}),baseURL:`http://127.0.0.1:${address.port}/v1`,connectionModel:'chat-model',requestTimeoutMs:10000,resolveCredential:()=>'sk-chat-test-key',maxResponseBytes:65536,maxStreamEventBytes:4096});
  const outcome = await query.invoke(call(received),request());
  expect(received).toEqual(['Hel','lo']);
  expect(outcome.kind).toBe('branch'); if (outcome.kind === 'branch') { expect(outcome.branch).toBe('generated'); expect(outcome.value.text).toBe('Hello'); expect(outcome.value.usage.totalTokens).toBe(3); }
});
it.each(['before headers','between event bytes'])('retries a provider stall %s', async phase => {
  server = createServer(async (incoming,response) => {
    for await (const chunk of incoming) { void chunk; }
    if (phase === 'between event bytes') { response.setHeader('Content-Type','text/event-stream'); response.write(': keep-alive\n\n'); }
    await new Promise<void>(resolve => setTimeout(resolve, 150));
    response.end('data: [DONE]\n\n');
  });
  await new Promise<void>(resolve => server?.listen(0,'127.0.0.1',resolve));
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('missing address');
  const query = newTextGenerationQuery({definition:{operation:{connectorId:'chat-lab',operationId:'generateText'},branches:textGenerationBranchDefinitions(),stepDefaults:{executeDurability:'sync',executeMethodTimeoutMs:60000}},wireFormat:newWireFormat({providerName:'chat-lab',streaming:'always',stallTimeoutMs:50}),baseURL:`http://127.0.0.1:${address.port}`,connectionModel:'chat-model',requestTimeoutMs:10000,resolveCredential:()=>'test-key',maxResponseBytes:65536,maxStreamEventBytes:4096});
  const outcome = await query.invoke(call(),request());
  expect(outcome.kind).toBe('retry');
  if (outcome.kind === 'retry') expect(outcome.failure.kind).toBe('AVAILABILITY');
});
it.each([
  ['interrupted','data: {"choices":[{"index":0,"delta":{"content":"partial"}}]}\n\n','retry','TRANSPORT'],
  ['reported error','data: {"error":{"code":"stream_failed","message":"secret provider text"}}\n\n','invalidResponse','PROTOCOL'],
  ['oversized event',`data: ${JSON.stringify({choices:[{index:0,delta:{content:'x'.repeat(5000)}}]})}\n\n`,'invalidResponse','RESPONSE_TOO_LARGE'],
] as const)('classifies %s stream',async (_name,body,expected,kind) => {
  server = createServer(async (incoming,response) => {
    for await (const chunk of incoming) { void chunk; }
    response.setHeader('Content-Type','text/event-stream'); response.end(body);
  });
  await new Promise<void>(resolve => server?.listen(0,'127.0.0.1',resolve));
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('missing address');
  const query = newTextGenerationQuery({definition:{operation:{connectorId:'chat-lab',operationId:'generateText'},branches:textGenerationBranchDefinitions(),stepDefaults:{executeDurability:'sync',executeMethodTimeoutMs:60000}},wireFormat:newWireFormat({providerName:'chat-lab',streaming:'always'}),baseURL:`http://127.0.0.1:${address.port}`,connectionModel:'chat-model',requestTimeoutMs:10000,resolveCredential:()=>'test-key',maxResponseBytes:65536,maxStreamEventBytes:4096});
  const outcome = await query.invoke(call(),request());
  expect(outcome.kind === 'retry' ? 'retry' : outcome.branch).toBe(expected);
  expect(outcome.failure?.kind).toBe(kind);
  expect(outcome.failure?.message).not.toContain('secret provider text');
});
