// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import "github.com/superdurable/dex-connectors-library/sdkgo/textgen"

// ConnectionGenerateTextQueryForTest returns the Query a Connection's generated Step runs, such as one NewProjectConnection opened.
func ConnectionGenerateTextQueryForTest(connection Connection) *textgen.TextGenerationQuery {
	return connection.client.GenerateText()
}
