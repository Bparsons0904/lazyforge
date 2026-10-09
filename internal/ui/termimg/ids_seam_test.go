package termimg

import (
	"fmt"
	"testing"
)

// All 240 IDs go out before any is reused, and the reuse takes the least recently acquired one.
func TestSeamIDsReuse(t *testing.T) {
	for _, tmux := range []bool{false, true} {
		ids := NewIDs(tmux)
		for i := range maxID - minID + 1 {
			key := fmt.Sprint("key", i)
			if id, fresh, del := ids.Acquire(key); id != minID+i || !fresh || del != "" {
				t.Fatalf("tmux=%v key %d: got (%d, %v, %q), want (%d, true, \"\")", tmux, i, id, fresh, del, minID+i)
			}
		}
		if _, fresh, _ := ids.Acquire("key0"); fresh {
			t.Fatalf("tmux=%v: re-acquiring a held key reported fresh", tmux)
		}
		id, fresh, del := ids.Acquire("extra")
		if want := minID + 1; id != want || !fresh || del != Delete(want, tmux) {
			t.Errorf("tmux=%v: 241st key got (%d, %v, %q), want (%d, true, Delete(%d))", tmux, id, fresh, del, want, want)
		}
	}
}
