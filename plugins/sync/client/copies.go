package client

// A lineage entry alone is historical metadata, not proof of an unfinished
// copy. A common baseline settles that copy unless a later operation is still
// uncertain. Ordinary new edits of settled copies retain all conflict choices.
func (r *record) copyNeedsReview(name string) bool {
	_, tracked := r.Copies[name]
	return tracked && (r.Operations[name] != nil || r.LegacyUnverified[name] || r.Baselines[name] == nil)
}

func (r *record) keepBothNeedsCopyReview(name string) bool {
	if r.copyNeedsReview(name) {
		return true
	}
	for copyPath, original := range r.Copies {
		if original == name && (r.copyNeedsReview(copyPath) || r.Conflicts[copyPath] != nil) {
			return true
		}
	}
	return false
}

func (r *record) conflictReason() string {
	reason := "CONFLICTS_PENDING"
	for name, conflict := range r.Conflicts {
		if r.copyNeedsReview(name) {
			return "CONFLICT_COPY_REVIEW_REQUIRED"
		}
		if conflict.Base == nil {
			reason = "INITIAL_SYNC_CHOICE_REQUIRED"
		}
	}
	return reason
}
