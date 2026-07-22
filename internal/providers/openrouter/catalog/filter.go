package catalog

func AgentModels(models []Model) []Model {
	out := make([]Model, 0, len(models))
	for _, m := range models {
		if m.AgentCompatible() {
			out = append(out, m)
		}
	}
	return out
}

func FreeModels(models []Model) []Model {
	out := make([]Model, 0)
	for _, m := range models {
		if m.AgentCompatible() && m.IsFree() {
			out = append(out, m)
		}
	}
	return out
}

func FindByID(models []Model, id string) (Model, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}
