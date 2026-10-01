// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

// ClientOfConnection exposes a Connection's client to the external tests.
func ClientOfConnection(connection Connection) *Client { return connection.client }
