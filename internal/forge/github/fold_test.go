package github

import (
	"strconv"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func TestFoldCI(t *testing.T) {
	run := func(status, conclusion string) checkRun { return checkRun{Status: status, Conclusion: conclusion} }
	none := combinedStatus{State: "pending", TotalCount: 0} // what GitHub returns for a commit with no statuses
	tests := []struct {
		name string
		runs []checkRun
		st   combinedStatus
		want domain.CIState
	}{
		{"nothing at all", nil, none, domain.CINone},
		{"empty combined pending is ignored", []checkRun{run("completed", "success")}, none, domain.CIPass},
		{"status only success", nil, combinedStatus{"success", 2}, domain.CIPass},
		{"status only pending", nil, combinedStatus{"pending", 1}, domain.CIPending},
		{"status error is fail", nil, combinedStatus{"error", 1}, domain.CIFail},
		{"status failure beats passing runs", []checkRun{run("completed", "success")}, combinedStatus{"failure", 1}, domain.CIFail},
		{"failed run beats passing status", []checkRun{run("completed", "timed_out")}, combinedStatus{"success", 3}, domain.CIFail},
		{"fail beats cancelled", []checkRun{run("completed", "cancelled"), run("completed", "action_required")}, none, domain.CIFail},
		{"startup_failure is fail", []checkRun{run("completed", "startup_failure")}, none, domain.CIFail},
		{"cancelled beats running", []checkRun{run("in_progress", ""), run("completed", "cancelled")}, none, domain.CICancelled},
		{"running beats pending", []checkRun{run("queued", ""), run("in_progress", "")}, combinedStatus{"pending", 1}, domain.CIRunning},
		{"pending beats pass", []checkRun{run("completed", "success"), run("waiting", "")}, none, domain.CIPending},
		{"requested is pending", []checkRun{run("requested", "")}, none, domain.CIPending},
		{"pending status beats passing runs", []checkRun{run("completed", "success")}, combinedStatus{"pending", 1}, domain.CIPending},
		{"neutral is pass", []checkRun{run("completed", "neutral"), run("completed", "skipped")}, none, domain.CIPass},
		{"skipped and stale only", []checkRun{run("completed", "skipped"), run("completed", "stale")}, none, domain.CISkipped},
		{"unknown conclusion is never green", []checkRun{run("completed", "brand_new")}, none, domain.CIPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := foldCI(tt.runs, tt.st); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestETagCacheEvictsOldest(t *testing.T) {
	c := newETagCache(3, 1<<20, 1<<20)
	for i := range 4 {
		c.put(strconv.Itoa(i), cachedEntry{etag: strconv.Itoa(i)})
	}
	c.put("1", cachedEntry{etag: "1b"}) // an update keeps its slot rather than growing the cache
	if _, ok := c.get("0"); ok {
		t.Error("oldest entry survived")
	}
	for _, k := range []string{"1", "2", "3"} {
		if _, ok := c.get(k); !ok {
			t.Errorf("entry %s evicted", k)
		}
	}
	if e, _ := c.get("1"); e.etag != "1b" || len(c.entries) != 3 {
		t.Errorf("entry 1 %+v, %d entries", e, len(c.entries))
	}
}

func TestETagCacheSkipsOversizedBody(t *testing.T) {
	c := newETagCache(10, 100, 10)
	c.put("a", cachedEntry{etag: "1", body: make([]byte, 5)})
	c.put("a", cachedEntry{etag: "2", body: make([]byte, 11)}) // over the per-entry limit: no stale body left behind
	c.put("b", cachedEntry{etag: "1", body: make([]byte, 11)})
	if _, ok := c.get("a"); ok {
		t.Error("stale entry a survived an oversized update")
	}
	if _, ok := c.get("b"); ok {
		t.Error("oversized body b was cached")
	}
	if c.bytes != 0 || len(c.order) != 0 {
		t.Errorf("bytes %d, order %v", c.bytes, c.order)
	}
}

func TestETagCacheByteBudgetEvictsOldest(t *testing.T) {
	c := newETagCache(10, 20, 10)
	for _, k := range []string{"a", "b", "c"} {
		c.put(k, cachedEntry{etag: k, body: make([]byte, 8)})
	}
	if _, ok := c.get("a"); ok {
		t.Error("oldest entry survived a byte budget overrun")
	}
	for _, k := range []string{"b", "c"} {
		if _, ok := c.get(k); !ok {
			t.Errorf("entry %s evicted", k)
		}
	}
	if c.bytes != 16 {
		t.Errorf("bytes %d, want 16", c.bytes)
	}
}
