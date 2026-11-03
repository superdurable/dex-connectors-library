// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError } from './index.js';

/** Incremental reader with Go's data-line and per-event size behavior. */
export class ServerSentEventDecoder {
  readonly #maxEventBytes: number;
  readonly #maxTotalBytes: number;
  #totalBytes = 0;
  #pending = Buffer.alloc(0);
  #eventBytes = 0;
  #dataLines: string[] = [];
  #eventType = '';

  constructor(maxEventBytes: number, maxTotalBytes = Number.MAX_SAFE_INTEGER) {
    this.#maxEventBytes = maxEventBytes;
    this.#maxTotalBytes = maxTotalBytes;
  }

  /** Decode complete events; false from onEvent stops the current response. */
  feed(chunk: Uint8Array, onEvent: (event: { data: string; event?: string }) => boolean | void): boolean {
    this.#pending = Buffer.concat([this.#pending, chunk]);
    for (;;) {
      const newline = this.#pending.indexOf(10);
      if (newline < 0) {
        if (this.#totalBytes + this.#pending.length > this.#maxTotalBytes) throw new BodyTooLargeError();
        if (this.#pending.length > this.#maxEventBytes + 2) throw new BodyTooLargeError();
        return true;
      }
      this.#totalBytes += newline + 1;
      if (this.#totalBytes > this.#maxTotalBytes) throw new BodyTooLargeError();
      let line = this.#pending.subarray(0, newline);
      this.#pending = this.#pending.subarray(newline + 1);
      const hasCR = line.at(-1) === 13;
      if (hasCR) line = line.subarray(0, line.length - 1);
      if (line.length > this.#maxEventBytes + (hasCR ? 0 : 1)) throw new BodyTooLargeError();
      if (line.length === 0) {
        if (this.#dataLines.length > 0) {
          const event = { data: this.#dataLines.join('\n'), ...(this.#eventType ? { event: this.#eventType } : {}) };
          this.#resetEvent();
          if (onEvent(event) === false) return false;
        } else this.#resetEvent();
        continue;
      }
      if (line[0] === 58) continue;
      this.#eventBytes += line.length;
      if (this.#eventBytes > this.#maxEventBytes) throw new BodyTooLargeError();
      const colon = line.indexOf(58);
      const field = (colon < 0 ? line : line.subarray(0, colon)).toString('utf8');
      let value = colon < 0 ? Buffer.alloc(0) : line.subarray(colon + 1);
      if (value[0] === 32) value = value.subarray(1);
      if (field === 'data') this.#dataLines.push(value.toString('utf8'));
      if (field === 'event') this.#eventType = value.toString('utf8');
    }
  }

  #resetEvent(): void {
    this.#eventBytes = 0;
    this.#dataLines = [];
    this.#eventType = '';
  }
}
