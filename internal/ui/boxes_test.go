package ui

import "testing"

// Seam: terminology. The change-request box is titled with the host's word.
func TestBoxTitleUsesHostTerm(t *testing.T) {
	for term, want := range map[string]string{"PR": "Pull requests", "MR": "Merge requests"} {
		if got := boxTitle(boxCRs, term); got != want {
			t.Errorf("boxTitle(boxCRs, %q) = %q, want %q", term, got, want)
		}
	}
}
