package runtime

import "testing"

func TestGuestNativeHandleRoundTrip(t *testing.T) {
	type box struct{ n int }
	guest := NewGuestHandleTable()
	host := NewHandleTable()
	proxies := map[uint64]Value{}

	hostForeign := func(id uint64) Value {
		if !isGuestHandleID(id) {
			return Value{}
		}
		if v, ok := proxies[id]; ok {
			return v
		}
		v := WireHandle(id)
		proxies[id] = v
		return v
	}
	guestForeign := func(id uint64) Value {
		if isGuestHandleID(id) {
			return Value{}
		}
		return WireHandle(id)
	}

	orig := Native(&box{n: 7})
	b, err := Encode(orig, guest)
	if err != nil {
		t.Fatal(err)
	}
	onHost, err := DecodeForeign(b, host, hostForeign)
	if err != nil {
		t.Fatal(err)
	}
	if onHost.Kind() != KindNative {
		t.Fatalf("kind %s", onHost.Kind())
	}
	if _, ok := wireHandleID(onHost); !ok {
		t.Fatal("host should see wire handle")
	}
	b2, err := Encode(onHost, host)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeForeign(b2, guest, guestForeign)
	if err != nil {
		t.Fatal(err)
	}
	var got *box
	if !back.As(&got) || got == nil || got.n != 7 {
		t.Fatalf("identity lost: %#v", back)
	}
	raw, ok := orig.Native()
	if !ok || raw.(*box) != got {
		t.Fatal("not same pointer")
	}
}
