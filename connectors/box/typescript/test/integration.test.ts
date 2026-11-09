// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { verifyBoxDownload } from '../examples/file-download/integration.js';

it('routes the Box mock through a real Dex Worker and Client', async () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  await verifyBoxDownload(address);
}, 60_000);
