// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

const numericBoardID = /^[1-9][0-9]{0,19}$/;
const boardAddress = /^https:\/\/[A-Za-z0-9-]+\.monday\.com\/boards\/([1-9][0-9]{0,19})(?:[/?#].*)?$/;

/** boardIDFromEntry accepts a numeric board ID or a board address and returns the ID, or "" when neither. */
export function boardIDFromEntry(entry: string): string {
  const trimmed = entry.trim();
  if (numericBoardID.test(trimmed)) return trimmed;
  return boardAddress.exec(trimmed)?.[1] ?? "";
}

/** savedBoardID returns a stored board ID, or "" for a missing or malformed value. */
export function savedBoardID(value: unknown): string {
  return typeof value === "string" && numericBoardID.test(value) ? value : "";
}
