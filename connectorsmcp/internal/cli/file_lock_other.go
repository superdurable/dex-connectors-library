// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build !unix

package cli

import (
	"errors"
	"os"
)

func tryLockFile(*os.File) (bool, error) {
	return false, errors.New("dex-connectors-mcp serve needs a Unix system")
}

func unlockAndCloseFile(file *os.File) error { return file.Close() }
