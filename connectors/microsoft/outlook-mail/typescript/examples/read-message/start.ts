// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp,rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { Client,Registry,openBlobCache } from '@superdurable/dex';
import { GeneratedMock } from '../../src/generated/mock.js';
import { ConnectionName,OutlookMessageFlow } from './flow.js';
const directory=await mkdtemp(join(tmpdir(),'outlook-mail-starter-'));const cache=openBlobCache({directory,maxBytes:64*1024*1024});const flow=new OutlookMessageFlow(new GeneratedMock(ConnectionName).connection());const client=new Client(new Registry([flow]),cache,{serverAddress:process.env.DEX_FLOW_SERVICE_ADDRESS??'127.0.0.1:8801'});
try{const flowId='outlook-mail-read-'+randomUUID();await client.startFlow(flow,flowId,{messageId:process.argv[2]??'AAMkAGI2MDEyMzQ1AAABC002'});process.stdout.write(flowId+'\n');}finally{await client.close();cache.close();await rm(directory,{recursive:true,force:true});}
