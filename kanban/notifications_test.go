package kanban

import "testing"

func TestNotificationDiffIncludesInlineChanges(t *testing.T) {
	diff := notificationDiff("card title", "new card title")
	if len(diff) != 2 {
		t.Fatalf("expected unified diff lines, got %d", len(diff))
	}
	var removed, added bool
	for _, line := range diff {
		if line.Class == "diff-removed" && len(line.Parts) > 0 {
			removed = true
		}
		if line.Class == "diff-added" && len(line.Parts) > 0 {
			added = true
		}
	}
	if !removed || !added {
		t.Fatalf("expected inline parts on removed and added lines: %#v", diff)
	}
}
