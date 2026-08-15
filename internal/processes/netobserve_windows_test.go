//go:build windows

package processes

import (
	"context"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"
)

func TestTcpOwnersReportsALiveConnectionForThisProcess(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()

	dialed, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer dialed.Close()

	conn := <-accepted
	defer conn.Close()

	pid := uint32(os.Getpid())
	deadline := time.Now().Add(2 * time.Second)
	var owners map[uint32][]netip.AddrPort
	for {
		owners, err = tcpOwners()
		if err != nil {
			t.Fatalf("tcpOwners: %v", err)
		}
		if len(owners[pid]) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tcpOwners never reported a connection owned by this process; owners[pid]=%v", owners[pid])
		}
		time.Sleep(20 * time.Millisecond)
	}

	found := false
	for _, ep := range owners[pid] {
		if ep.Addr().Is4() && ep.Addr().IsLoopback() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a loopback remote endpoint for pid %d, got %v", pid, owners[pid])
	}
}

func TestJobProcessIDsListsAConfinedChild(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "job-pid-list-membership",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_HANG=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{t.TempDir()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Terminate(50 * time.Millisecond) }()

	jc, ok := process.confinement.(*jobConfinement)
	if !ok {
		t.Fatalf("process.confinement = %T, want *jobConfinement", process.confinement)
	}

	pids, err := jobProcessIDs(jc.job)
	if err != nil {
		t.Fatalf("jobProcessIDs: %v", err)
	}
	want := uint32(process.PID())
	for _, pid := range pids {
		if pid == want {
			return
		}
	}
	t.Fatalf("jobProcessIDs = %v, want it to contain %d", pids, want)
}

func TestJobProcessIDsGrowsTheBufferWhenTheListDoesNotFit(t *testing.T) {
	original := initialJobProcessIDCapacity
	defer func() { initialJobProcessIDCapacity = original }()
	initialJobProcessIDCapacity = 1

	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "job-pid-list-growth",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_SPAWN_MANY=20"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, MaxProcesses: 64, WritableRoots: []string{t.TempDir()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Terminate(500 * time.Millisecond) }()

	jc, ok := process.confinement.(*jobConfinement)
	if !ok {
		t.Fatalf("process.confinement = %T, want *jobConfinement", process.confinement)
	}

	deadline := time.Now().Add(3 * time.Second)
	var pids []uint32
	for {
		pids, err = jobProcessIDs(jc.job)
		if err != nil {
			t.Fatalf("jobProcessIDs: %v", err)
		}
		if len(pids) >= 15 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never accumulated enough member processes with a 1-entry starting buffer; last count=%d", len(pids))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
