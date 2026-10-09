package markdown

import (
	"slices"
	"testing"
)

// TestImagesReportsOnlyTopLevelBlockImages checks which destinations Images reports, against the shapes the ticket names.
func TestImagesReportsOnlyTopLevelBlockImages(t *testing.T) {
	for _, tc := range []struct {
		body string
		want []string
	}{
		{"see ![a](x.png) here", nil},
		{"- ![a](x.png)", nil},
		{"> ![a](x.png)", nil},
		{"<img src=x.png>", nil},
		{"![a](x.png)<br>", nil},
		{"[![a](x.png)](https://e)", []string{"x.png"}},
		{"![a](x.png) ![b](y.png)\n\n![c](x.png)", []string{"x.png", "y.png"}},
		{"![a](x.png)\n![b](y.png)", []string{"x.png", "y.png"}},
	} {
		if got := Images(tc.body); !slices.Equal(got, tc.want) {
			t.Errorf("Images(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}
