// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Cross-process credential refresh lock compatible with sdkgo's `connections.json.lock`.
//!
//! The protocol is Go's `fileRefreshLock`: an exclusive `flock(2)` on the never-replaced,
//! owner-only file named by appending `.lock` to the connections file name. The kernel
//! releases the lock when its descriptor closes or the holder dies, so there is no stale
//! state, content, or PID in the file. A per-store mutex orders threads first, as Go's
//! semaphore does.

use std::ffi::OsString;
use std::fs::File;
use std::path::{Path, PathBuf};
use std::sync::{Mutex, MutexGuard, TryLockError};
use std::time::{Duration, Instant};

use super::errors::{ProjectConfigError, ProjectConfigResult};

/// Suffix appended to the connections file name, as Go's `refreshLockFileSuffix`.
const REFRESH_LOCK_FILE_SUFFIX: &str = ".lock";
/// Bounds a wait so a stopped holder cannot block a call forever.
const REFRESH_LOCK_MAX_WAIT: Duration = Duration::from_secs(120);
const REFRESH_LOCK_FIRST_POLL_DELAY: Duration = Duration::from_millis(5);
const REFRESH_LOCK_MAX_POLL_DELAY: Duration = Duration::from_millis(100);

/// Exclusive refresh lock of one connections file.
pub(crate) struct FileRefreshLock {
    path: PathBuf,
    max_wait: Duration,
    in_process: Mutex<()>,
}

/// One acquisition; dropping it closes the descriptor, then releases the in-process mutex.
pub(crate) struct HeldRefreshLock<'lock> {
    // Field order matters: the file lock is released before the mutex.
    _file: Option<File>,
    _in_process: MutexGuard<'lock, ()>,
}

impl FileRefreshLock {
    /// Returns the lock beside `connections_path`; it neither creates nor opens the file.
    pub(crate) fn beside(connections_path: &Path) -> Self {
        let mut name = OsString::from(connections_path.as_os_str());
        name.push(REFRESH_LOCK_FILE_SUFFIX);
        Self {
            path: PathBuf::from(name),
            max_wait: REFRESH_LOCK_MAX_WAIT,
            in_process: Mutex::new(()),
        }
    }

    /// Waits for the in-process mutex, then the file lock, for at most two minutes.
    ///
    /// A timeout or lock-file failure is [`ProjectConfigError::RefreshFailed`].
    pub(crate) fn acquire(&self) -> ProjectConfigResult<HeldRefreshLock<'_>> {
        let deadline = Instant::now() + self.max_wait;
        let mut delay = REFRESH_LOCK_FIRST_POLL_DELAY;
        let guard = loop {
            match self.in_process.try_lock() {
                Ok(guard) => break guard,
                // The guarded value is `()`, so a poisoned lock holds no broken state.
                Err(TryLockError::Poisoned(poisoned)) => break poisoned.into_inner(),
                Err(TryLockError::WouldBlock) => {
                    wait_for_poll(deadline, &mut delay)?;
                }
            }
        };
        let file = lock_file_exclusively(&self.path, deadline, &mut delay)?;
        Ok(HeldRefreshLock {
            _file: file,
            _in_process: guard,
        })
    }
}

fn wait_for_poll(deadline: Instant, delay: &mut Duration) -> ProjectConfigResult<()> {
    let now = Instant::now();
    if now >= deadline {
        return Err(ProjectConfigError::RefreshFailed(
            "wait for the credential refresh lock timed out".into(),
        ));
    }
    std::thread::sleep((*delay).min(deadline - now));
    *delay = (*delay * 2).min(REFRESH_LOCK_MAX_POLL_DELAY);
    Ok(())
}

fn lock_error(message: &str) -> ProjectConfigError {
    ProjectConfigError::RefreshFailed(format!("the credential refresh lock file {message}"))
}

/// Polls a non-blocking `flock(2)` until it holds the lock.
#[cfg(unix)]
fn lock_file_exclusively(
    path: &Path,
    deadline: Instant,
    delay: &mut Duration,
) -> ProjectConfigResult<Option<File>> {
    use rustix::fs::{FlockOperation, Mode, OFlags, flock, open};
    // NOFOLLOW refuses a planted link; NONBLOCK keeps a planted FIFO from blocking the open.
    let descriptor = open(
        path,
        OFlags::RDWR | OFlags::CREATE | OFlags::NOFOLLOW | OFlags::NONBLOCK | OFlags::CLOEXEC,
        Mode::from_raw_mode(0o600),
    )
    .map_err(|_| lock_error("cannot be opened"))?;
    let file = File::from(descriptor);
    require_private_lock_file(&file)?;
    loop {
        match flock(&file, FlockOperation::NonBlockingLockExclusive) {
            Ok(()) => return Ok(Some(file)),
            Err(error)
                if error == rustix::io::Errno::WOULDBLOCK || error == rustix::io::Errno::INTR =>
            {
                wait_for_poll(deadline, delay)?;
            }
            Err(_) => return Err(lock_error("cannot be locked")),
        }
    }
}

/// Accepts only a regular lock file of this user, removing group and other access.
#[cfg(unix)]
fn require_private_lock_file(file: &File) -> ProjectConfigResult<()> {
    use std::os::unix::fs::{MetadataExt, PermissionsExt};
    let metadata = file
        .metadata()
        .map_err(|_| lock_error("cannot be inspected"))?;
    if !metadata.file_type().is_file() {
        return Err(lock_error("is not a regular file"));
    }
    if metadata.uid() != rustix::process::geteuid().as_raw() {
        return Err(lock_error("is not owned by the current user"));
    }
    if metadata.permissions().mode() & 0o077 != 0 {
        file.set_permissions(std::fs::Permissions::from_mode(0o600))
            .map_err(|_| lock_error("permissions cannot be restricted to 0600"))?;
    }
    Ok(())
}

/// Takes no file lock off Unix, where Go coordinates one loaded project only, too.
#[cfg(not(unix))]
fn lock_file_exclusively(
    _path: &Path,
    _deadline: Instant,
    _delay: &mut Duration,
) -> ProjectConfigResult<Option<File>> {
    Ok(None)
}
