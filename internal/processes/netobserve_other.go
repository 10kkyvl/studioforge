//go:build !windows

package processes

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"time"
)

const DefaultNetworkObservationInterval = time.Second

type NetworkObservation struct {
	Supported bool
	Endpoints []netip.AddrPort
}

type NetworkObserver interface {
	Supported() bool
	Sample() ([]netip.AddrPort, error)
}

func ObserveNetwork(ctx context.Context, observer NetworkObserver, interval time.Duration) NetworkObservation {
	if !observer.Supported() {
		return NetworkObservation{}
	}
	if interval <= 0 {
		interval = DefaultNetworkObservationInterval
	}
	seen := map[netip.AddrPort]struct{}{}
	sample := func() {
		endpoints, err := observer.Sample()
		if err != nil {
			slog.Debug("network observation sample failed", "error", err)
			return
		}
		for _, ep := range endpoints {
			if isGloballyRoutable(ep.Addr()) {
				seen[ep] = struct{}{}
			}
		}
	}
	sample()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return finalizeObservation(seen)
		case <-ticker.C:
			sample()
		}
	}
}

func finalizeObservation(seen map[netip.AddrPort]struct{}) NetworkObservation {
	endpoints := make([]netip.AddrPort, 0, len(seen))
	for ep := range seen {
		endpoints = append(endpoints, ep)
	}
	return NetworkObservation{Supported: true, Endpoints: endpoints}
}

func isGloballyRoutable(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return false
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	return true
}

var ErrNetworkObservationUnavailable = errors.New("processes: network observation is not determinable on this platform")

type unsupportedObserver struct{}

func (unsupportedObserver) Supported() bool { return false }

func (unsupportedObserver) Sample() ([]netip.AddrPort, error) {
	return nil, ErrNetworkObservationUnavailable
}

func NewProcessNetworkObserver(process *Process) NetworkObserver {
	return unsupportedObserver{}
}
