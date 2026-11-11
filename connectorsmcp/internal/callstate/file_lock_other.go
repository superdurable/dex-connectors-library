// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build !unix

package callstate

import (
	"context"
	"errors"
	"os"
	"time"
)

// errFileLockUnsupported keeps keyed writes off systems without a verified lock.
var errFileLockUnsupported = errors.New("call state file locks need a Unix system")

func lockKeyFile(context.Context, string, time.Duration) (*os.File, error) {
	return nil, errFileLockUnsupported
}

// tryLockFile is unsupported outside Unix systems.
func tryLockFile(*os.File) (bool, error) { return false, errFileLockUnsupported }

func unlockAndCloseKeyFile(file *os.File) error { return file.Close() }
