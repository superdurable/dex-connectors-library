// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dailydigest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SummarizeWithCodex invokes the locally authenticated Codex CLI with structured output.
// It uses an empty temporary directory and a read-only sandbox. It does not copy or expose credentials.
// The CLI must be installed and logged in before the Worker starts. Each call has a four-minute timeout.
func SummarizeWithCodex(ctx context.Context, prompt string) (selections []Selection, err error) {
	directory, err := os.MkdirTemp("", "hacker-news-digest-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(directory)) }()
	schemaPath := filepath.Join(directory, "digest.schema.json")
	outputPath := filepath.Join(directory, "digest.json")
	if err := os.WriteFile(schemaPath, []byte(selectionSchema), 0600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "codex", "exec", "--ignore-user-config", "--ephemeral", "--sandbox", "read-only",
		"--skip-git-repo-check", "--disable", "shell_tool", "--disable", "apps", "--disable", "plugins", "--disable", "multi_agent",
		"-c", `web_search="disabled"`, "--output-schema", schemaPath, "--output-last-message", outputPath, "-")
	command.Dir = directory
	command.Stdin = strings.NewReader(prompt)
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("codex exec failed; check CLI installation and login status: %w", err)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	var response struct {
		Items []Selection `json:"items"`
	}
	if err := json.Unmarshal(contents, &response); err != nil {
		return nil, fmt.Errorf("decode Codex digest: %w", err)
	}
	return response.Items, nil
}

const selectionSchema = `{
  "type":"object", "additionalProperties":false, "required":["items"],
  "properties":{"items":{"type":"array","items":{
    "type":"object","additionalProperties":false,
    "required":["id","summary","why","discussion","commentIds"],
    "properties":{
      "id":{"type":"integer"}, "summary":{"type":"string"},
      "why":{"type":"string"}, "discussion":{"type":"string"},
      "commentIds":{"type":"array","items":{"type":"integer"}}
    }
  }}}
}`
