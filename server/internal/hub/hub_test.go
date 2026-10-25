package hub

import "testing"

func TestFanoutIsolatedByChannel(t *testing.T) {
	h := New()
	a, cancelA := h.Subscribe("301")
	defer cancelA()
	b, cancelB := h.Subscribe("301")
	defer cancelB()
	c, cancelC := h.Subscribe("302")
	defer cancelC()

	if n := h.Publish("301", []byte("hi")); n != 2 {
		t.Fatalf("delivered %d", n)
	}
	if string(<-a) != "hi" || string(<-b) != "hi" {
		t.Fatal("payload")
	}
	select {
	case msg := <-c:
		t.Fatalf("leaked %s", msg)
	default:
	}
}
