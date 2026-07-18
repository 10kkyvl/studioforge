package config

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ProductName = "StudioForge"
	AppID       = "studioforge"
	Repository  = "https://github.com/10kkyvl/studioforge"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

type Options struct {
	Host        string
	Port        int
	DataDir     string
	NoOpen      bool
	LogLevel    string
	SafeMode    bool
	MockMode    bool
	UnsafeHost  bool
	Command     string
	CommandArgs []string
}

func (o *Options) Normalize() error {
	if o.Host == "" {
		o.Host = "127.0.0.1"
	}
	if o.LogLevel == "" {
		o.LogLevel = "info"
	}
	if o.Port < 0 || o.Port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	if !o.UnsafeHost && !IsLoopbackHost(o.Host) {
		return errors.New("non-loopback --host requires --unsafe-host; remote exposure is not recommended")
	}
	if o.DataDir != "" {
		abs, err := filepath.Abs(o.DataDir)
		if err != nil {
			return fmt.Errorf("resolve data directory: %w", err)
		}
		o.DataDir = filepath.Clean(abs)
	}
	switch strings.ToLower(o.LogLevel) {
	case "debug", "info", "warn", "error":
		o.LogLevel = strings.ToLower(o.LogLevel)
	default:
		return fmt.Errorf("unsupported log level %q", o.LogLevel)
	}
	return nil
}

func IsLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func UserAgent() string {
	return fmt.Sprintf("%s/%s (%s/%s)", AppID, Version, runtime.GOOS, runtime.GOARCH)
}
