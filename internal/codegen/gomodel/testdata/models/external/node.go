// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package external

// Node is an external recursive JSON model.
type Node struct {
	Next *Node `json:"next"`
}
