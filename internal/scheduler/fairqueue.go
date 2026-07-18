package scheduler

type fairQueue struct {
	byProject map[string][]*Job
	order     []string
	cursor    int
}

func newFairQueue() *fairQueue { return &fairQueue{byProject: map[string][]*Job{}} }
func (q *fairQueue) push(j *Job) {
	if len(q.byProject[j.ProjectID]) == 0 {
		q.order = append(q.order, j.ProjectID)
	}
	q.byProject[j.ProjectID] = append(q.byProject[j.ProjectID], j)
}
func (q *fairQueue) pop(can func(*Job) bool) *Job {
	if len(q.order) == 0 {
		return nil
	}
	for attempts := 0; attempts < len(q.order); attempts++ {
		if q.cursor >= len(q.order) {
			q.cursor = 0
		}
		project := q.order[q.cursor]
		jobs := q.byProject[project]
		if len(jobs) > 0 && can(jobs[0]) {
			job := jobs[0]
			q.byProject[project] = jobs[1:]
			if len(q.byProject[project]) == 0 {
				delete(q.byProject, project)
				q.order = append(q.order[:q.cursor], q.order[q.cursor+1:]...)
				if q.cursor >= len(q.order) {
					q.cursor = 0
				}
			} else {
				q.cursor = (q.cursor + 1) % len(q.order)
			}
			return job
		}
		q.cursor = (q.cursor + 1) % len(q.order)
	}
	return nil
}

// remove takes a job back out of the queue before it ever starts, returning it
// so the caller can record the run's terminal state. Cancelling needs this:
// under the concurrency ceilings a run can sit queued for a long time, and it
// has no goroutine and no provider process to signal, so the only way to stop
// it is to drop it from the queue.
func (q *fairQueue) remove(runID string) (*Job, bool) {
	for project, jobs := range q.byProject {
		for i, job := range jobs {
			if job.RunID != runID {
				continue
			}
			// Cap the prefix so append copies instead of writing into the array
			// pop still shares with earlier slices of the same project.
			q.byProject[project] = append(jobs[:i:i], jobs[i+1:]...)
			if len(q.byProject[project]) == 0 {
				delete(q.byProject, project)
				q.forget(project)
			}
			return job, true
		}
	}
	return nil, false
}

// forget drops an emptied project from the round-robin order. The cursor is an
// index into that order, so entries removed at or before it have to shift it
// back, otherwise removal would silently skip whichever project moved into the
// vacated slot.
func (q *fairQueue) forget(project string) {
	for i, id := range q.order {
		if id != project {
			continue
		}
		q.order = append(q.order[:i], q.order[i+1:]...)
		if q.cursor > i {
			q.cursor--
		}
		if q.cursor >= len(q.order) {
			q.cursor = 0
		}
		return
	}
}
