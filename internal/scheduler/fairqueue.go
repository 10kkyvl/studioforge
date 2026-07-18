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
