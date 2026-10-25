package store

import (
	"bytes"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAddListAndImage(t *testing.T) {
	s := testStore(t)
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	msg, err := s.add("三年二班", "请翻开第12页", "image/png", png, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(msg.ID) {
		t.Fatalf("id %s", msg.ID)
	}
	got, err := s.Get(msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "请翻开第12页" || got.Channel != "三年二班" || got.ImageMime != "image/png" {
		t.Fatalf("%+v", got)
	}
	f, err := s.OpenImage(msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, len(png))
	if _, err := f.Read(buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, png) {
		t.Fatal("image bytes")
	}
	other, err := s.List("301", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Messages) != 0 {
		t.Fatal("channel leak")
	}
}

func TestPaginationSameMillisecond(t *testing.T) {
	s := testStore(t)
	for _, body := range []string{"a", "b", "c"} {
		if _, err := s.add("301", body, "", nil, 1000); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.List("301", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 2 || page.NextBefore == 0 {
		t.Fatalf("%+v", page)
	}
	if page.Messages[0].Body != "c" || page.Messages[1].Body != "b" {
		t.Fatalf("order %+v", page.Messages)
	}
	rest, err := s.List("301", page.NextBefore, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Messages) != 1 || rest.Messages[0].Body != "a" || rest.NextBefore != 0 {
		t.Fatalf("%+v", rest)
	}
}

func TestOpenImageRejectsTraversal(t *testing.T) {
	s := testStore(t)
	if _, err := s.OpenImage("../" + filepath.Base(s.imgDir)); err == nil {
		t.Fatal("expected reject")
	}
	if _, err := s.Get("not-an-id"); !errorsIsNotFound(err) {
		t.Fatal(err)
	}
}

func errorsIsNotFound(err error) bool {
	return err == ErrNotFound
}
