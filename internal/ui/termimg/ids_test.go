package termimg_test

import (
	"strconv"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

const idCount = 240 // IDs 16 through 255

func key(i int) string { return "key-" + strconv.Itoa(i) }

// fillIDs acquires idCount distinct keys on a and checks each gets a fresh ID in order, with nothing to delete.
func fillIDs(t *testing.T, a *termimg.IDs) {
	t.Helper()
	for i := range idCount {
		id, fresh, del := a.Acquire(key(i))
		if id != 16+i || !fresh || del != "" {
			t.Fatalf("Acquire(%s) = (%d, %t, %q), want (%d, true, \"\")", key(i), id, fresh, del, 16+i)
		}
	}
}

func TestIDsFirstAcquireIsFreshAndHeld(t *testing.T) {
	a := termimg.NewIDs(false)
	if id, fresh, del := a.Acquire("a"); id != 16 || !fresh || del != "" {
		t.Fatalf("first Acquire = (%d, %t, %q), want (16, true, \"\")", id, fresh, del)
	}
	if id, fresh, del := a.Acquire("a"); id != 16 || fresh || del != "" {
		t.Fatalf("second Acquire of the same key = (%d, %t, %q), want (16, false, \"\")", id, fresh, del)
	}
}

func TestIDsFillTheTable(t *testing.T) {
	a := termimg.NewIDs(false)
	fillIDs(t, a)
}

func TestIDsReuseLeastRecentlyAcquired(t *testing.T) {
	for _, tmux := range []bool{false, true} {
		a := termimg.NewIDs(tmux)
		fillIDs(t, a)
		id, fresh, del := a.Acquire("new")
		if id != 16 || !fresh {
			t.Fatalf("tmux=%t: Acquire of a new key = (%d, %t, _), want (16, true, _)", tmux, id, fresh)
		}
		if want := termimg.Delete(16, tmux); del != want {
			t.Errorf("tmux=%t: del = %q, want %q", tmux, del, want)
		}
	}
}

func TestIDsReacquireMovesKeyToMostRecent(t *testing.T) {
	a := termimg.NewIDs(false)
	fillIDs(t, a)
	if id, fresh, del := a.Acquire(key(0)); id != 16 || fresh || del != "" {
		t.Fatalf("re-acquire of key-0 = (%d, %t, %q), want (16, false, \"\")", id, fresh, del)
	}
	id, fresh, del := a.Acquire("new")
	if id != 17 || !fresh {
		t.Fatalf("Acquire of a new key after re-acquiring key-0 = (%d, %t, _), want (17, true, _)", id, fresh)
	}
	if want := termimg.Delete(17, false); del != want {
		t.Errorf("del = %q, want %q", del, want)
	}
	if id, fresh, _ := a.Acquire(key(0)); id != 16 || fresh {
		t.Errorf("key-0 after the reuse = (%d, %t), want (16, false)", id, fresh)
	}
}

func TestIDsEvictedKeyIsNewAgain(t *testing.T) {
	a := termimg.NewIDs(false)
	fillIDs(t, a)
	a.Acquire("new")
	if _, fresh, _ := a.Acquire(key(0)); !fresh {
		t.Error("evicted key-0 acquired fresh = false, want true")
	}
}
