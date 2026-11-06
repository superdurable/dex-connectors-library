// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** Compute Microsoft's 160-bit QuickXorHash for conflict read-back. */
export function computeQuickXorHash(content: Uint8Array): string {
  const block = new Uint8Array(20);
  for (let index = 0; index < content.length; index++) {
    const value = content[index]!;
    const bitPosition = (index * 11) % 160;
    const byteIndex = Math.floor(bitPosition / 8), bitOffset = bitPosition % 8;
    block[byteIndex] = block[byteIndex]! ^ (value << bitOffset & 0xff);
    if (bitOffset) block[(byteIndex + 1) % 20] = block[(byteIndex + 1) % 20]! ^ (value >> (8 - bitOffset));
  }
  for (let index = 0; index < 8; index++) block[12 + index] = block[12 + index]! ^ (Math.floor(content.length / 2 ** (8 * index)) & 0xff);
  return Buffer.from(block).toString('base64');
}
