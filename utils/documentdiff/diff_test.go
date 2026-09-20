package documentdiff

import "testing"

func TestCompareKeepsUnchangedAndShowsEdits(t *testing.T) {
	result := Compare("标题\n原段落", "标题\n新段落")
	if len(result) != 3 || result[0].Kind != Unchanged || result[1].Kind != Removed || result[2].Kind != Added {
		t.Fatalf("unexpected diff: %#v", result)
	}
}
