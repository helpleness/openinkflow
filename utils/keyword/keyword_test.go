package keyword

import "testing"

func TestNormalizeSplitsChineseQuery(t *testing.T) {
	keywords := Normalize([]string{"给我简单介绍一下三升四劫难和天赋是什么"})
	terms := map[string]bool{}
	for _, keyword := range keywords {
		terms[keyword] = true
	}
	for _, want := range []string{"三升四", "劫难", "天赋"} {
		if !terms[want] {
			t.Fatalf("expected keyword %q in %v", want, keywords)
		}
	}
}

func TestBuildQueryTextLimitsRunes(t *testing.T) {
	got := BuildQueryText([]string{"alpha", "beta", "gamma"}, 10)
	if got != "alpha beta" {
		t.Fatalf("BuildQueryText = %q", got)
	}
}
