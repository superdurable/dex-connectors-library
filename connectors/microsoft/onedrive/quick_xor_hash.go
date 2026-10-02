// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import "encoding/base64"

const (
	quickXorHashWidthBits = 160
	quickXorHashShiftBits = 11
	quickXorHashBytes     = quickXorHashWidthBits / 8
	// quickXorHashLengthOffset is where the little-endian content length is folded in.
	quickXorHashLengthOffset = quickXorHashBytes - 8
)

// computeQuickXorHash XORs byte i at bit 11*i mod 160, then folds in the little-endian length.
func computeQuickXorHash(content []byte) string {
	var block [quickXorHashBytes]byte
	for index, value := range content {
		bitPosition := (uint64(index) * quickXorHashShiftBits) % quickXorHashWidthBits
		byteIndex, bitOffset := bitPosition/8, bitPosition%8
		block[byteIndex] ^= value << bitOffset
		if bitOffset != 0 {
			block[(byteIndex+1)%quickXorHashBytes] ^= value >> (8 - bitOffset)
		}
	}
	length := uint64(len(content))
	for index := range 8 {
		block[quickXorHashLengthOffset+index] ^= byte(length >> (8 * index))
	}
	return base64.StdEncoding.EncodeToString(block[:])
}
