// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { verifyMeetingList } from '../../examples/meeting-list/integration.js';

it('runs the Zoom meeting-list Flow through a real Dex Worker and Client', async () => {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new TypeError('DEX_FLOW_SERVICE_ADDRESS is required for the Dex integration test');
  await verifyMeetingList(serverAddress);
}, 60_000);
