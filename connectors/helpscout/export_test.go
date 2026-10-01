// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

// ClientOfConnection gives the external tests the client a local connection constructor built.
func ClientOfConnection(connection Connection) *Client { return connection.client }
