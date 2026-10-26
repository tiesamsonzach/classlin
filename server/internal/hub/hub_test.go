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
	pa, pb := <-a, <-b
	if string(pa.Data) != "hi" || string(pb.Data) != "hi" {
		t.Fatal("payload")
	}
	if pa.Event != "message" || pb.Event != "message" {
		t.Fatalf("event %q %q", pa.Event, pb.Event)
	}
	select {
	case msg := <-c:
		t.Fatalf("leaked %s", msg.Data)
	default:
	}
}

func TestPublishEventName(t *testing.T) {
	h := New()
	ch, cancel := h.Subscribe("301")
	defer cancel()
	if n := h.PublishEvent("301", "signal", []byte(`{"kind":"hangup"}`)); n != 1 {
		t.Fatalf("delivered %d", n)
	}
	pkt := <-ch
	if pkt.Event != "signal" || string(pkt.Data) != `{"kind":"hangup"}` {
		t.Fatalf("%+v", pkt)
	}
}
