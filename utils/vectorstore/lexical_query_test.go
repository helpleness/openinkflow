package vectorstore

import (
	"strings"
	"testing"
)

func TestLexicalQueryTermsKeepWholePhraseAndIndividualTerms(t *testing.T) {
	query := "林晓雨  员工编号 E1001 直属上级"
	terms := LexicalQueryTerms(query)
	if len(terms) == 0 || terms[0] != "林晓雨 员工编号 E1001 直属上级" {
		t.Fatalf("full phrase was not preserved first: %#v", terms)
	}
	want := map[string]bool{"林晓雨": false, "员工编号": false, "E1001": false, "直属上级": false}
	for _, term := range terms {
		if _, exists := want[term]; exists {
			want[term] = true
		}
	}
	for term, found := range want {
		if !found {
			t.Fatalf("missing individual term %q in %#v", term, terms)
		}
	}
	matchQuery := sqliteFTS5MatchQuery(query)
	for _, phrase := range []string{`"林晓雨 员工编号 E1001 直属上级"`, `"林晓雨"`, `"员工编号"`, `"E1001"`, `"直属上级"`} {
		if !strings.Contains(matchQuery, phrase) {
			t.Fatalf("FTS query %q is missing %s", matchQuery, phrase)
		}
	}
}

func TestLexicalQueryTermsExposeEmbeddedCJKAndIdentifierTerms(t *testing.T) {
	terms := LexicalQueryTerms("查询员工为林晓雨，员工编号为E1001")
	want := map[string]bool{"林晓雨": false, "E1001": false}
	for _, term := range terms {
		if _, exists := want[term]; exists {
			want[term] = true
		}
	}
	for term, found := range want {
		if !found {
			t.Fatalf("missing %q in %#v", term, terms)
		}
	}
}

func TestLexicalQueryTermsAreBoundedAndUnique(t *testing.T) {
	terms := LexicalQueryTerms("这是一个很长的自然语言查询用于验证检索词不会无限增长 这是一个很长的自然语言查询用于验证检索词不会无限增长")
	if len(terms) > maxLexicalQueryTerms {
		t.Fatalf("too many terms: %d", len(terms))
	}
	seen := map[string]bool{}
	for _, term := range terms {
		if seen[term] {
			t.Fatalf("duplicate term %q in %#v", term, terms)
		}
		seen[term] = true
	}
}
