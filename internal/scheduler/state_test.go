package scheduler

import "testing"

func TestStateMachine(t *testing.T) {
	valid := [][2]string{{"queued", "waiting_resources"}, {"queued", "failed"}, {"waiting_resources", "starting"}, {"waiting_resources", "failed"}, {"starting", "running"}, {"running", "paused"}, {"paused", "running"}, {"paused", "failed"}, {"running", "completed"}, {"running", "cancelling"}, {"cancelling", "cancelled"}, {"running", "interrupted"}, {"interrupted", "queued"}}
	for _, pair := range valid {
		if err := ValidateTransition(pair[0], pair[1]); err != nil {
			t.Errorf("expected %v valid: %v", pair, err)
		}
	}
	invalid := [][2]string{{"completed", "running"}, {"queued", "completed"}, {"failed", "completed"}, {"cancelled", "running"}}
	for _, pair := range invalid {
		if err := ValidateTransition(pair[0], pair[1]); err == nil {
			t.Errorf("expected %v invalid", pair)
		}
	}
}
func TestFairQueueRoundRobin(t *testing.T) {
	q := newFairQueue()
	for i := 0; i < 3; i++ {
		q.push(&Job{RunID: string(rune('a' + i)), ProjectID: "p1"})
	}
	q.push(&Job{RunID: "x", ProjectID: "p2"})
	q.push(&Job{RunID: "y", ProjectID: "p3"})
	got := []string{}
	for i := 0; i < 5; i++ {
		got = append(got, q.pop(func(*Job) bool { return true }).ProjectID)
	}
	want := []string{"p1", "p2", "p3", "p1", "p1"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order=%v want=%v", got, want)
		}
	}
}
