// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build !darwin && !linux

package projectconfig

import (
	"context"
	"os"
)

// isCrossProcessRefreshLockSupported reports whether this platform coordinates refresh across processes.
const isCrossProcessRefreshLockSupported = false

// lockFileExclusively takes no file lock here, so refresh is coordinated within one loaded project only.
func lockFileExclusively(context.Context, string) (*os.File, error) {
	return nil, nil
}

// syncDirectory does nothing on this platform, where a directory cannot be opened for synchronization.
func syncDirectory(string) error {
	return nil
}
