//go:build windows

package processes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
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
	jc, ok := process.confinement.(*jobConfinement)
	if !ok || jc == nil {
		return unsupportedObserver{}
	}
	jc.mu.Lock()
	job := jc.job
	jc.mu.Unlock()
	if job == 0 {
		return unsupportedObserver{}
	}
	return jobNetworkObserver{job: job}
}

type jobNetworkObserver struct {
	job windows.Handle
}

func (o jobNetworkObserver) Supported() bool { return true }

func (o jobNetworkObserver) Sample() ([]netip.AddrPort, error) {
	pids, err := jobProcessIDs(o.job)
	if err != nil {
		return nil, err
	}
	owners, err := tcpOwners()
	if err != nil {
		return nil, err
	}
	pidSet := make(map[uint32]struct{}, len(pids))
	for _, pid := range pids {
		pidSet[pid] = struct{}{}
	}
	var endpoints []netip.AddrPort
	for pid, addrs := range owners {
		if _, ok := pidSet[pid]; !ok {
			continue
		}
		endpoints = append(endpoints, addrs...)
	}
	return endpoints, nil
}

type jobObjectBasicProcessIDListHeader struct {
	NumberOfAssignedProcesses uint32
	NumberOfProcessIdsInList  uint32
}

var initialJobProcessIDCapacity uint32 = 64

func jobProcessIDs(job windows.Handle) ([]uint32, error) {
	count := initialJobProcessIDCapacity
	headerSize := uint32(unsafe.Sizeof(jobObjectBasicProcessIDListHeader{}))
	for attempt := 0; attempt < 8; attempt++ {
		size := headerSize + count*uint32(unsafe.Sizeof(uintptr(0)))
		buf := make([]byte, size)
		var retlen uint32
		err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList,
			uintptr(unsafe.Pointer(&buf[0])), uint32(len(buf)), &retlen)
		header := (*jobObjectBasicProcessIDListHeader)(unsafe.Pointer(&buf[0]))
		if err == nil {
			ids := unsafe.Slice((*uintptr)(unsafe.Pointer(&buf[headerSize])), header.NumberOfProcessIdsInList)
			pids := make([]uint32, len(ids))
			for i, id := range ids {
				pids[i] = uint32(id)
			}
			return pids, nil
		}
		if errors.Is(err, windows.ERROR_MORE_DATA) {
			count = header.NumberOfAssignedProcesses + 16
			continue
		}
		return nil, fmt.Errorf("query job process id list: %w", err)
	}
	return nil, fmt.Errorf("query job process id list: exceeded retry limit growing the buffer")
}

const tcpTableOwnerPIDAll = 5

const (
	mibTCPStateClosed    = 1
	mibTCPStateListen    = 2
	mibTCPStateDeleteTCB = 12
)

func tcpStateIndicatesConnection(state uint32) bool {
	switch state {
	case mibTCPStateListen, mibTCPStateClosed, mibTCPStateDeleteTCB:
		return false
	default:
		return true
	}
}

type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPid  uint32
}

type mibTCP6RowOwnerPID struct {
	LocalAddr     [16]byte
	LocalScopeID  uint32
	LocalPort     uint32
	RemoteAddr    [16]byte
	RemoteScopeID uint32
	RemotePort    uint32
	State         uint32
	OwningPid     uint32
}

var procGetExtendedTcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func fetchTCPTable(family uint32) ([]byte, error) {
	size := uint32(8 * 1024)
	for attempt := 0; attempt < 8; attempt++ {
		buf := make([]byte, size)
		r0, _, _ := procGetExtendedTcpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			0,
			uintptr(family),
			uintptr(tcpTableOwnerPIDAll),
			0,
		)
		switch syscall.Errno(r0) {
		case 0:
			return buf, nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			continue
		default:
			return nil, fmt.Errorf("GetExtendedTcpTable: %w", syscall.Errno(r0))
		}
	}
	return nil, fmt.Errorf("GetExtendedTcpTable: exceeded retry limit growing the buffer")
}

func ntohsFromDword(v uint32) uint16 {
	return uint16(v&0xff)<<8 | uint16((v>>8)&0xff)
}

func parseTCPv4Owners(buf []byte, owners map[uint32][]netip.AddrPort) {
	if len(buf) < 4 {
		return
	}
	numEntries := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := unsafe.Sizeof(mibTCPRowOwnerPID{})
	available := uint32((uint64(len(buf)) - 4) / uint64(rowSize))
	if numEntries > available {
		numEntries = available
	}
	rows := unsafe.Slice((*mibTCPRowOwnerPID)(unsafe.Pointer(&buf[4])), numEntries)
	for _, row := range rows {
		if !tcpStateIndicatesConnection(row.State) {
			continue
		}
		addr := netip.AddrFrom4([4]byte{byte(row.RemoteAddr), byte(row.RemoteAddr >> 8), byte(row.RemoteAddr >> 16), byte(row.RemoteAddr >> 24)})
		port := ntohsFromDword(row.RemotePort)
		owners[row.OwningPid] = append(owners[row.OwningPid], netip.AddrPortFrom(addr, port))
	}
}

func parseTCPv6Owners(buf []byte, owners map[uint32][]netip.AddrPort) {
	if len(buf) < 4 {
		return
	}
	numEntries := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := unsafe.Sizeof(mibTCP6RowOwnerPID{})
	available := uint32((uint64(len(buf)) - 4) / uint64(rowSize))
	if numEntries > available {
		numEntries = available
	}
	rows := unsafe.Slice((*mibTCP6RowOwnerPID)(unsafe.Pointer(&buf[4])), numEntries)
	for _, row := range rows {
		if !tcpStateIndicatesConnection(row.State) {
			continue
		}
		addr := netip.AddrFrom16(row.RemoteAddr)
		port := ntohsFromDword(row.RemotePort)
		owners[row.OwningPid] = append(owners[row.OwningPid], netip.AddrPortFrom(addr, port))
	}
}

func tcpOwners() (map[uint32][]netip.AddrPort, error) {
	owners := map[uint32][]netip.AddrPort{}
	v4, err := fetchTCPTable(windows.AF_INET)
	if err != nil {
		return nil, fmt.Errorf("fetch ipv4 tcp table: %w", err)
	}
	parseTCPv4Owners(v4, owners)
	v6, err := fetchTCPTable(windows.AF_INET6)
	if err != nil {
		return nil, fmt.Errorf("fetch ipv6 tcp table: %w", err)
	}
	parseTCPv6Owners(v6, owners)
	return owners, nil
}
