// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { spawn,execFile,type ChildProcess } from 'node:child_process';
import { promisify } from 'node:util';
import { createConnection } from 'node:net';
import { mkdtemp,rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { afterAll,beforeAll,expect,it } from 'vitest';
import { Client,Registry,Worker,openBlobCache,type BlobCache } from '@superdurable/dex';
import { GeneratedMock,getMessageMessageMissing } from '../../src/generated/mock.js';
import { ConnectionName,OutlookMessageFlow } from './flow.js';

const dexPort=Number(process.env.DEX_TEST_PORT ?? '40950');
const webPort=Number(process.env.DEX_TEST_WEB_PORT ?? '40951');
const serviceAddress=`127.0.0.1:${dexPort}`;
const execFileAsync=promisify(execFile);
const mock=new GeneratedMock(ConnectionName);
const flow=new OutlookMessageFlow(mock.connection());
const registry=new Registry([flow]);
let directory:string;let cache:BlobCache;let client:Client;let worker:Worker;let server:ChildProcess;
async function waitForPort(port:number,child:ChildProcess):Promise<void>{const deadline=Date.now()+30_000;while(Date.now()<deadline){if(child.exitCode!==null)throw new Error('dexcli dev exited before startup');const connected=await new Promise<boolean>(resolve=>{const socket=createConnection({host:'127.0.0.1',port});socket.once('connect',()=>{socket.destroy();resolve(true);});socket.once('error',()=>{socket.destroy();resolve(false);});});if(connected){try{await execFileAsync('dexcli',['version','check','-server',serviceAddress,'-timeout','2s']);return;}catch{/* The FlowService is still starting. */}}await new Promise(resolve=>setTimeout(resolve,100));}throw new Error('dexcli dev did not open its port');}
beforeAll(async()=>{directory=await mkdtemp(join(tmpdir(),'outlook-mail-dex-'));server=spawn('dexcli',['dev','-open=false','-dex-port',String(dexPort),'-web-port',String(webPort),'-zd-data-dir',join(directory,'server')],{stdio:'ignore'});await waitForPort(dexPort,server);cache=openBlobCache({directory:join(directory,'cache'),maxBytes:64*1024*1024});client=new Client(registry,cache,{serverAddress:serviceAddress});const deadline=Date.now()+30000;while(true){worker=new Worker(registry,cache,{serverAddress:serviceAddress});try{await worker.start();break;}catch(error){await worker.close().catch(()=>{});if(Date.now()>deadline)throw error;await new Promise(resolve=>setTimeout(resolve,100));}}await worker.waitPullRegistered({timeoutMs:15000});},45_000);
afterAll(async()=>{await client?.close();await worker?.close();cache?.close();if(server){server.kill('SIGTERM');await new Promise<void>(resolve=>{if(server.exitCode!==null||server.signalCode!==null)resolve();else server.once('exit',()=>resolve());});}if(directory)await rm(directory,{recursive:true,force:true});},30_000);
it('reads a message through a real Dex Worker and retains the result',async()=>{const id='outlook-read-'+randomUUID();await client.startFlow(flow,id,{messageId:'AAMkAGI2MDEyMzQ1AAABC002'});const result=await client.waitForFlow(id,20000);expect(result.status).toBe('completed');expect(result.singleOutput()).toMatchObject({id:'AAMkAGI2MDEyMzQ1AAABC002'});expect(mock.getMessage().calls().some(call=>call.flowId===id)).toBe(true);},30000);
it('fails the Flow when an optional notFound branch is unwired',async()=>{const id='outlook-missing-'+randomUUID();mock.getMessage().forFlow(id).respond(getMessageMessageMissing());await client.startFlow(flow,id,{messageId:'missing'});const result=await client.waitForFlow(id,20000);expect(result.status).toBe('failed');},30000);
