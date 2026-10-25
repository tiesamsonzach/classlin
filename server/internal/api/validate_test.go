package api

import "testing"

func TestValidChannel(t *testing.T) {
	ok := []string{"301", "class-3", "三年二班", "A_1", "3.2"}
	bad := []string{"", " ", "三年 二班", "a/b", "a\\b", "a?b", "a&b", stringsRepeat("a", 33)}
	for _, s := range ok {
		if !ValidChannel(s) {
			t.Fatalf("want ok %q", s)
		}
	}
	for _, s := range bad {
		if ValidChannel(s) {
			t.Fatalf("want bad %q", s)
		}
	}
}

func TestValidToken(t *testing.T) {
	if !ValidToken("") || !ValidToken("Ab12") || !ValidToken("a-b_c.1") {
		t.Fatal("expected valid")
	}
	if ValidToken("有口令") || ValidToken("a b") || ValidToken(stringsRepeat("a", 65)) {
		t.Fatal("expected invalid")
	}
}

func stringsRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
