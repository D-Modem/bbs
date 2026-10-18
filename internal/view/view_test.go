package view

import "testing"

// TestLayout: columns laid out wide and narrow (the wide-only one left
// out, the widths fitting), and cells cut and padded to their widths.
func TestLayout(t *testing.T) {
	cols := []Column{{Title: "Area", Width: 20}, {Title: "Description", Width: 36, Wide: true},
		{Title: "Msgs", Width: 5, Right: true}, {Title: "Doing"}}
	idx, w := Layout(cols, false, 79)
	if len(idx) != 4 || w[0] != 20 || w[1] != 36 || w[2] != 5 || w[3] != 79-20-36-5-3 {
		t.Errorf("wide: %v %v", idx, w)
	}
	idx, w = Layout(cols, true, 38)
	if len(idx) != 3 || idx[1] != 2 || w[0]+w[1]+w[2]+2 > 38 {
		t.Errorf("narrow: %v %v", idx, w)
	}
	if got := Cells(cols, []int{0, 2}, []int{6, 5}, []string{"General area", "", "12"}); got != "Genera    12" {
		t.Errorf("cells %q", got)
	}
}
