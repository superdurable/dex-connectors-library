// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import WebSocket from 'ws';

/** One open Slack Socket Mode connection. */
export interface SlackSocketConnection {
  /**
   * Resolve the next JSON message. Reject when the connection ends, a message is not JSON, or neither a
   * message nor a ping arrives for readTimeoutMs; Slack pings idle connections, and every ping extends the wait.
   */
  readMessage(readTimeoutMs: number): Promise<unknown>;
  /** Send one JSON message. */
  writeMessage(value: unknown): Promise<void>;
  /** Close the connection; closing twice is harmless. */
  close(): void;
}

/** Open a Socket Mode WebSocket URL; errors must not contain the URL, which carries a connection ticket. */
export type SlackSocketDialer = (url: string, signal: AbortSignal) => Promise<SlackSocketConnection>;

/** Default largest Socket Mode message in bytes, the Slack client's default response limit. */
const defaultMaxSocketMessageBytes = 4_194_304;
/** Largest message limit ws honors; it stores maxPayload as a signed 32-bit integer. */
export const MaxSlackSocketMessageBytes = 2 ** 31 - 1;

/** Error whose message never names the Socket Mode URL. */
class SocketModeConnectionError extends Error {
  constructor(message: string) { super(message); this.name = 'SocketModeConnectionError'; }
}

/**
 * Dial a Socket Mode URL with the ws package; ws answers every ping with a pong automatically. A message larger
 * than maxMessageBytes, a positive integer of at most MaxSlackSocketMessageBytes, ends the connection. A signal
 * already aborted rejects without connecting. While a received envelope waits to be read, the connection pauses
 * the socket after the network chunk being parsed, so a reader blocked on a full delivery queue holds at most
 * that chunk's envelopes and stops pulling from Slack instead of buffering a flood.
 */
export async function dialSlackSocket(url: string, signal: AbortSignal, maxMessageBytes = defaultMaxSocketMessageBytes): Promise<SlackSocketConnection> {
  if (!Number.isSafeInteger(maxMessageBytes) || maxMessageBytes < 1 || maxMessageBytes > MaxSlackSocketMessageBytes) throw new TypeError('Slack Socket Mode message limit must be a positive integer of at most 2147483647 bytes');
  if (signal.aborted) throw new SocketModeConnectionError('connect Slack Socket Mode: aborted');
  const socket = new WebSocket(url, { handshakeTimeout: 30_000, maxPayload: maxMessageBytes });
  // Listen before the handshake completes, or frames arriving with the 101 response, such as Slack's hello, are lost.
  const connection = new WebSocketConnection(socket, url);
  const onAbort = () => socket.terminate();
  signal.addEventListener('abort', onAbort, { once: true });
  try {
    await new Promise<void>((resolve, reject) => {
      socket.once('open', () => resolve());
      socket.once('error', error => reject(new SocketModeConnectionError(`connect Slack Socket Mode: ${redactSocketURL(error.message, url)}`)));
      socket.once('close', () => reject(new SocketModeConnectionError('connect Slack Socket Mode: connection closed during the handshake')));
    });
  } finally { signal.removeEventListener('abort', onAbort); }
  return connection;
}

class WebSocketConnection implements SlackSocketConnection {
  readonly #messages: unknown[] = [];
  #failure: Error | undefined;
  #waiter: { resolve(): void; extend(): void } | undefined;

  constructor(private readonly socket: WebSocket, url: string) {
    socket.on('message', (data, isBinary) => {
      if (isBinary) { this.#fail(new SocketModeConnectionError('read Slack Socket Mode envelope: binary message')); return; }
      try { this.#messages.push(JSON.parse(rawText(data)) as unknown); }
      catch { this.#fail(new SocketModeConnectionError('read Slack Socket Mode envelope: invalid JSON')); return; }
      // Stop pulling from Slack while an envelope waits; the chunk already being parsed still finishes.
      socket.pause();
      this.#waiter?.resolve();
    });
    socket.on('ping', () => this.#waiter?.extend());
    socket.on('error', error => this.#fail(new SocketModeConnectionError(`read Slack Socket Mode envelope: ${redactSocketURL(error.message, url)}`)));
    socket.on('close', code => this.#fail(new SocketModeConnectionError(`read Slack Socket Mode envelope: connection closed (${code})`)));
  }

  /** Envelopes received and not yet read; the socket pauses while any wait. */
  get pendingMessageCount(): number { return this.#messages.length; }

  async readMessage(readTimeoutMs: number): Promise<unknown> {
    for (;;) {
      if (this.#messages.length > 0) {
        const message = this.#messages.shift();
        if (this.#messages.length === 0) this.socket.resume();
        return message;
      }
      if (this.#failure) throw this.#failure;
      await new Promise<void>(resolve => {
        let timer = setTimeout(onTimeout, readTimeoutMs);
        const finish = () => { clearTimeout(timer); this.#waiter = undefined; resolve(); };
        function onTimeout() { finish(); }
        this.#waiter = { resolve: finish, extend: () => { clearTimeout(timer); timer = setTimeout(onTimeout, readTimeoutMs); } };
        if (this.#messages.length > 0 || this.#failure) finish();
      });
      if (this.#messages.length === 0 && !this.#failure) {
        this.close();
        throw new SocketModeConnectionError('read Slack Socket Mode envelope: read timeout');
      }
    }
  }

  async writeMessage(value: unknown): Promise<void> {
    if (this.#failure) throw this.#failure;
    await new Promise<void>((resolve, reject) => this.socket.send(JSON.stringify(value), error => error ? reject(new SocketModeConnectionError('write Slack Socket Mode message failed')) : resolve()));
  }

  close(): void { this.socket.terminate(); }

  #fail(error: Error): void {
    this.#failure ??= error;
    this.#waiter?.resolve();
  }
}

function rawText(data: WebSocket.RawData): string {
  if (Array.isArray(data)) return Buffer.concat(data).toString('utf8');
  return Buffer.from(data as ArrayBuffer).toString('utf8');
}

/** Remove the Socket Mode URL from text; the URL carries a connection ticket that must not reach a log record. */
export function redactSocketURL(text: string, url: string): string {
  return url ? text.split(url).join('[Socket Mode URL]') : text;
}
