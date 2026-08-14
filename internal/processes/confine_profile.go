package processes

// sandboxProfile returns the SBPL (Sandbox Profile Language) text used to
// confine agent-started processes on macOS via sandbox-exec. Paths are never
// interpolated into this string; they are passed at run time as -D ROOT=...,
// -D TMP=..., -D HOME=... parameters to sandbox-exec, and referenced here only
// via (param "...").
func sandboxProfile() string {
	return `(version 1)
(allow default)
(deny file-write*)
(allow file-write*
  (subpath (param "ROOT"))
  (subpath (param "TMP"))
  (subpath "/private/var/folders")
  (subpath "/private/tmp")
  (subpath "/tmp")
  (subpath (string-append (param "HOME") "/Library/Caches"))
  (subpath (string-append (param "HOME") "/Library/Developer"))
  (subpath (string-append (param "HOME") "/.cache"))
  (subpath (string-append (param "HOME") "/.npm"))
  (subpath (string-append (param "HOME") "/.cargo"))
  (subpath (string-append (param "HOME") "/go/pkg/mod"))
  (subpath (string-append (param "HOME") "/go/pkg/sumdb")))
(allow file-write-data
  (literal "/dev/null")
  (literal "/dev/zero")
  (literal "/dev/random")
  (literal "/dev/urandom")
  (literal "/dev/tty")
  (literal "/dev/dtracehelper")
  (regex #"^/dev/fd/"))
(allow file-ioctl
  (literal "/dev/tty")
  (regex #"^/dev/ttys[0-9]*"))
`
}
