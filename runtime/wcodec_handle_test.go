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
	got, ok := back.As[*box]()
	if !ok || got == nil || got.n != 7 {
		t.Fatalf("identity lost: %#v", back)
	}
	raw, ok := orig.Native()
	if !ok || raw.(*box) != got {
		t.Fatal("not same pointer")
	}
}

// stampPeer mimics wasmInst.HandleID for guestRef-shaped natives.
type stampPeer struct {
	self    *HandleTable
	ownerID uint64 // pointer identity stand-in: use map key
	mine    map[uint64]Value
}

type fakeOwner struct{ id int }

type stamped struct {
	owner *fakeOwner
	id    uint64
}

func (p *stampPeer) HandleID(v Value) (uint64, bool) {
	if v.k != KindNative {
		return 0, false
	}
	s, ok := v.p.(*stamped)
	if !ok || s == nil {
		return 0, false
	}
	if s.owner.id == int(p.ownerID) {
		return s.id, true
	}
	return p.self.Put(v), true
}

func TestStampedCrossPeerPassThrough(t *testing.T) {
	type box struct{ n int }
	guestA := NewGuestHandleTable()
	hostA := NewHandleTable()
	hostB := NewHandleTable()
	ownerA := &fakeOwner{id: 1}
	ownerB := &fakeOwner{id: 2}
	peerA := &stampPeer{self: hostA, ownerID: 1, mine: map[uint64]Value{}}
	peerB := &stampPeer{self: hostB, ownerID: 2, mine: map[uint64]Value{}}

	orig := Native(&box{n: 3})
	raw, err := Encode(orig, guestA)
	if err != nil {
		t.Fatal(err)
	}
	// Decode onto host as stamped ref owned by A.
	var localID uint64
	onHost, err := DecodeForeign(raw, hostA, func(id uint64) Value {
		if !isGuestHandleID(id) {
			return Value{}
		}
		localID = id
		return Native(&stamped{owner: ownerA, id: id})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Pass through B: encode with peer B imports into hostB.
	toB, err := EncodePeer(onHost, hostB, peerB)
	if err != nil {
		t.Fatal(err)
	}
	onB, err := DecodeForeign(toB, NewGuestHandleTable(), func(id uint64) Value {
		if isGuestHandleID(id) {
			return Value{}
		}
		return WireHandle(id)
	})
	if err != nil {
		t.Fatal(err)
	}
	backHost, err := Encode(onB, nil) // wireHandle emits id
	if err != nil {
		t.Fatal(err)
	}
	// Host B decodes: lookup in hostB
	restored, err := DecodeForeign(backHost, hostB, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := restored.p.(*stamped)
	if !ok || s.owner != ownerA || s.id != localID {
		t.Fatalf("lost owner stamp: %#v", restored.p)
	}

	// Back to A: peer A emits local guest id.
	toA, err := EncodePeer(restored, hostA, peerA)
	if err != nil {
		t.Fatal(err)
	}
	final, err := DecodeForeign(toA, guestA, func(id uint64) Value {
		if isGuestHandleID(id) {
			return Value{}
		}
		return WireHandle(id)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := final.As[*box]()
	if !ok || got == nil || got.n != 3 {
		t.Fatalf("owner unwrap: %#v", final)
	}
	_ = ownerB
}
