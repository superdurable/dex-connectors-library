# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft QuickXorHash for conflict read-back."""

import base64


def compute_quick_xor_hash(content: bytes) -> str:
    """XOR byte i at bit 11*i modulo 160, then fold in the content length."""
    block = bytearray(20)
    for index, value in enumerate(content):
        bit_position = (index * 11) % 160
        byte_index, bit_offset = divmod(bit_position, 8)
        block[byte_index] ^= (value << bit_offset) & 0xFF
        if bit_offset:
            block[(byte_index + 1) % 20] ^= value >> (8 - bit_offset)
    for index in range(8):
        block[12 + index] ^= (len(content) >> (8 * index)) & 0xFF
    return base64.b64encode(block).decode("ascii")
