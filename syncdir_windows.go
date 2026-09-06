package main

// saveImage already calls File.Sync, which invokes Windows FlushFileBuffers on
// the writable image handle before the database commit. Windows does not support
// Unix-style fsync on Go's read-only directory handles: it returns access denied.
// Do not turn a successfully flushed image into an HTTP 500 by doing that here.
func syncImageDirectory(string) error { return nil }
