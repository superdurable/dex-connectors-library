// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package provider

type Request struct {
	Required string `json:"required"`
	Optional string `json:"optional,omitempty"`
	Zero     int    `json:"zero,omitzero"`
	Hidden   string `json:"-"`
}
