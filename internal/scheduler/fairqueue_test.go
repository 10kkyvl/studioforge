package scheduler

import "testing"

// Cancel relies on remove pulling a run out of the queue entirely: a job left
// behind would start after the run had already been marked cancelled, and one
// removed carelessly would corrupt the round-robin state pop walks.
func TestFairQueueRemove(t *testing.T) {
	type queued struct{ run, project string }
	cases := []struct {
		name        string
		push        []queued
		popsBefore  int
		remove      []string
		wantRemoved []bool
		wantDrain   []string
	}{
		{
			name:        "removes the only queued job",
			push:        []queued{{"a", "p1"}},
			remove:      []string{"a"},
			wantRemoved: []bool{true},
		},
		{
			name:        "removes from the middle of a project without disturbing the rest",
			push:        []queued{{"a", "p1"}, {"b", "p1"}, {"c", "p1"}},
			remove:      []string{"b"},
			wantRemoved: []bool{true},
			wantDrain:   []string{"a", "c"},
		},
		{
			name:        "unknown run is reported as a miss",
			push:        []queued{{"a", "p1"}},
			remove:      []string{"nope"},
			wantRemoved: []bool{false},
			wantDrain:   []string{"a"},
		},
		{
			name:        "emptied project leaves the rotation",
			push:        []queued{{"a", "p1"}, {"b", "p2"}, {"c", "p3"}},
			remove:      []string{"b"},
			wantRemoved: []bool{true},
			wantDrain:   []string{"a", "c"},
		},
		{
			// The cursor indexes the project order, so dropping a project at or
			// before it must not make the rotation skip the next project.
			name:        "rotation resumes at the right project after a removal",
			push:        []queued{{"a1", "p1"}, {"a2", "p1"}, {"b1", "p2"}, {"c1", "p3"}},
			popsBefore:  1,
			remove:      []string{"b1"},
			wantRemoved: []bool{true},
			wantDrain:   []string{"c1", "a2"},
		},
		{
			name:        "removing every job empties the queue",
			push:        []queued{{"a", "p1"}, {"b", "p2"}},
			remove:      []string{"a", "b"},
			wantRemoved: []bool{true, true},
		},
		{
			// A run that already started is no longer the queue's business; the
			// scheduler must fall through to its active-run cancel path.
			name:        "an already popped job is no longer removable",
			push:        []queued{{"a", "p1"}},
			popsBefore:  1,
			remove:      []string{"a"},
			wantRemoved: []bool{false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := newFairQueue()
			for _, item := range tc.push {
				q.push(&Job{RunID: item.run, ProjectID: item.project})
			}
			for i := 0; i < tc.popsBefore; i++ {
				if job := q.pop(func(*Job) bool { return true }); job == nil {
					t.Fatalf("setup pop %d found nothing to pop", i)
				}
			}
			for i, runID := range tc.remove {
				job, removed := q.remove(runID)
				if removed != tc.wantRemoved[i] {
					t.Fatalf("remove(%q)=%v want %v", runID, removed, tc.wantRemoved[i])
				}
				if removed && job.RunID != runID {
					t.Fatalf("remove(%q) returned job %q; Cancel needs the job to record its terminal state", runID, job.RunID)
				}
				if !removed && job != nil {
					t.Fatalf("remove(%q) returned a job on a miss: %+v", runID, job)
				}
			}
			drained := []string{}
			for {
				job := q.pop(func(*Job) bool { return true })
				if job == nil {
					break
				}
				drained = append(drained, job.RunID)
			}
			if len(drained) != len(tc.wantDrain) {
				t.Fatalf("drained=%v want=%v", drained, tc.wantDrain)
			}
			for i := range tc.wantDrain {
				if drained[i] != tc.wantDrain[i] {
					t.Fatalf("drained=%v want=%v", drained, tc.wantDrain)
				}
			}
			if len(q.order) != 0 || len(q.byProject) != 0 {
				t.Fatalf("drained queue still holds state: order=%v byProject=%v", q.order, q.byProject)
			}
		})
	}
}
