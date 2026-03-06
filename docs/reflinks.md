# Efficient File Copying on Linux with Reflinks

Reference for the `internal/copyfile` package implementation.

## Strategy

Try methods in order of speed, falling back on failure:

1. **FICLONE ioctl** — instant CoW clone (metadata-only operation)
2. **io.Copy** between `*os.File` — Go internally uses `copy_file_range(2)` then `splice(2)` then userspace copy

We skip explicit `copy_file_range` calls because Go's `io.Copy` already does
this automatically when both arguments are `*os.File` (via `os.(*File).ReadFrom`
→ `poll.CopyFileRange`).

## FICLONE

- **Ioctl number:** `0x40049409` (available as `unix.FICLONE` in `golang.org/x/sys/unix`)
- **Usage:** `unix.IoctlSetInt(int(dst.Fd()), unix.FICLONE, int(src.Fd()))`
- **Requirements:** both files on same filesystem, source open for reading, destination open for writing (not O_APPEND)
- **Supported filesystems:** Btrfs, XFS (with reflink=1, default since mkfs.xfs 5.1.0), OCFS2, ZFS (2.2.0+)
- **Available since:** Linux 4.5

## Fallback errors

Errors that mean "not supported, try next method":

| Error      | Meaning                                  |
|------------|------------------------------------------|
| `ENOSYS`   | Kernel too old                           |
| `EOPNOTSUPP` | Filesystem doesn't support reflinks    |
| `EXDEV`    | Files on different filesystems           |
| `EINVAL`   | Not regular files, or other constraint   |
| `EPERM`    | Blocked by container security policy     |

## Metadata copying

After copying file data, restore metadata using standard library:

```go
info, _ := os.Stat(src)
os.Chmod(dst, info.Mode().Perm())
os.Chtimes(dst, time.Now(), info.ModTime())
// uid/gid — non-fatal if it fails (requires root)
sys := info.Sys().(*syscall.Stat_t)
os.Chown(dst, int(sys.Uid), int(sys.Gid))
```

## Go's internal zero-copy chain

When `io.Copy(dstFile, srcFile)` is called with two `*os.File`:

```
io.Copy → os.(*File).ReadFrom → poll.CopyFileRange (copy_file_range syscall)
                                → poll.Splice (fallback)
                                → io.CopyBuffer (userspace fallback)
```

This means a simple `io.Copy` between files is already quite efficient — we
only need explicit FICLONE for the instant CoW case.
