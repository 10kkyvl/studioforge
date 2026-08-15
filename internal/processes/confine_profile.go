package processes

import "fmt"

// sandboxProfile returns the SBPL (Sandbox Profile Language) text used to
// confine agent-started processes on macOS via sandbox-exec. Paths are never
// interpolated into this string; they are passed at run time as -D ROOT=...,
// -D TMP=..., -D HOME=... parameters to sandbox-exec, and referenced here only
// via (param "...").
func sandboxProfile(net NetworkPolicy, proxyAddr string) (string, error) {
	rules, err := networkRules(net, proxyAddr)
	if err != nil {
		return "", err
	}
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
` + rules, nil
}

func sandboxNetworkOnlyProfile(net NetworkPolicy, proxyAddr string) (string, error) {
	rules, err := networkRules(net, proxyAddr)
	if err != nil {
		return "", err
	}
	return "(version 1)\n(allow default)\n" + rules, nil
}

func networkRules(net NetworkPolicy, proxyAddr string) (string, error) {
	switch net.Normalized() {
	case NetworkUnrestricted:
		return "", nil
	case NetworkNone:
		return "(deny network*)\n", nil
	case NetworkRegistryOnly:
		if proxyAddr == "" {
			return "", fmt.Errorf("processes: network policy %q requires a proxy address", NetworkRegistryOnly)
		}
		return "(deny network*)\n(allow network-outbound\n  (remote ip (param \"PROXY\")))\n", nil
	default:
		return "", fmt.Errorf("processes: unknown network policy %q", net)
	}
}
