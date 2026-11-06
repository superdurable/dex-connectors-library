// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp,rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { Registry,Worker,openBlobCache } from '@superdurable/dex';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName,OutlookMessageFlow } from './flow.js';
/** Run the Outlook Mail Flow Worker with the project connection. */
export async function runOutlookWorker():Promise<void>{const project=await loadFromEnvironment();const connection=newProjectConnection(project,ConnectionName);const directory=await mkdtemp(join(tmpdir(),'outlook-mail-worker-'));const cache=openBlobCache({directory,maxBytes:64*1024*1024});const worker=new Worker(new Registry([new OutlookMessageFlow(connection)]),cache,{serverAddress:process.env.DEX_FLOW_SERVICE_ADDRESS??'127.0.0.1:8801'});try{await worker.run();}finally{await worker.close();cache.close();await rm(directory,{recursive:true,force:true});}}
if(import.meta.url===new URL(`file://${process.argv[1]}`).href)runOutlookWorker().catch(()=>{process.stderr.write('Outlook Mail Worker failed\n');process.exitCode=1;});
