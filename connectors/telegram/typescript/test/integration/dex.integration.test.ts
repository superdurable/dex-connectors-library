// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { Mock } from '../../src/mock.js';
import type { Chat, SentMessage } from '../../src/index.js';
import { ChatLookupFlow, ConnectionName } from '../../examples/provider-operations/chat-lookup-flow.js';
import { MessageSendFlow } from '../../examples/provider-operations/message-send-flow.js';

it('runs both Telegram operations through real Dex Workers and Clients', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const directory = await mkdtemp(join(tmpdir(), 'dex-telegram-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const mock = new Mock(ConnectionName);
  const sendFlow = new MessageSendFlow(mock.connection());
  const chatFlow = new ChatLookupFlow(mock.connection());
  const registry = new Registry([sendFlow, chatFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const sendFlowId = `telegram-typescript-send-${randomUUID()}`;
    await client.startFlow(sendFlow, sendFlowId, { chatId: '-100123456', text: 'Hello from Dex' });
    const sent = await client.waitForFlow(sendFlowId, 60_000);
    expect(sent.status, sent.errorMessage).toBe('completed');
    expect(sent.singleOutput<SentMessage>()).toMatchObject({ messageId: 52 });
    expect(mock.sendMessage().calls()).toHaveLength(1);

    const chatFlowId = `telegram-typescript-chat-${randomUUID()}`;
    await client.startFlow(chatFlow, chatFlowId, { chatId: '-100123456' });
    const chat = await client.waitForFlow(chatFlowId, 60_000);
    expect(chat.status, chat.errorMessage).toBe('completed');
    expect(chat.singleOutput<Chat>()).toMatchObject({ type: 'supergroup' });
    expect(mock.getChat().calls()).toHaveLength(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
