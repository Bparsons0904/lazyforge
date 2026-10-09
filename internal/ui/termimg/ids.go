package termimg

// IDs assigns image IDs to keys; it is not safe for concurrent use.
type IDs struct {
	tmux  bool
	clock uint64
	byKey map[string]int
	slots [maxID + 1]idSlot
}

type idSlot struct {
	key  string
	held bool
	last uint64 // clock value of the slot's latest Acquire
}

// NewIDs returns an empty allocator; tmux wraps the Delete sequences it returns.
func NewIDs(tmux bool) *IDs {
	return &IDs{tmux: tmux, byKey: map[string]int{}}
}

// Acquire returns key's image ID. fresh is true when the ID was just assigned to key, so the
// caller must Transmit before drawing; del is the Delete sequence for the image the ID last
// held when an ID was reused, else "".
func (a *IDs) Acquire(key string) (id int, fresh bool, del string) {
	a.clock++
	if held, ok := a.byKey[key]; ok {
		a.slots[held].last = a.clock
		return held, false, ""
	}
	id = a.free()
	if id == 0 {
		id = a.oldest()
		del = Delete(id, a.tmux)
		delete(a.byKey, a.slots[id].key)
	}
	a.slots[id] = idSlot{key: key, held: true, last: a.clock}
	a.byKey[key] = id
	return id, true, del
}

// free returns the lowest unheld ID, or 0 when all are held.
func (a *IDs) free() int {
	for id := minID; id <= maxID; id++ {
		if !a.slots[id].held {
			return id
		}
	}
	return 0
}

// oldest returns the held ID with the earliest latest Acquire.
func (a *IDs) oldest() int {
	best := minID
	for id := minID + 1; id <= maxID; id++ {
		if a.slots[id].last < a.slots[best].last {
			best = id
		}
	}
	return best
}
