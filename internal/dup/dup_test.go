package dup_test

import (
	"fmt"
	"testing"

	"github.com/Travis-Britz/dedup/internal/dup"
)

func TestOffset(t *testing.T) {

	cases := map[int][]struct{ r, c, i int }{
		2: {
			{0, 1, 0},
		},
		3: {
			{0, 1, 0},
			{0, 2, 1},
			{1, 2, 2},
		},
		4: {
			{0, 1, 0},
			{0, 2, 1},
			{0, 3, 2},
			{1, 2, 3},
			{1, 3, 4},
			{2, 3, 5},
		},
		5: {
			{0, 1, 0},
			{0, 2, 1},
			{0, 3, 2},
			{0, 4, 3},
			{1, 2, 4},
			{1, 3, 5},
			{1, 4, 6},
			{2, 3, 7},
			{2, 4, 8},
			{3, 4, 9},
		},
		6: {
			{0, 1, 0},
			{0, 2, 1},
			{1, 2, 5},
		},
	}

	for n, tt := range cases {
		for _, tc := range tt {
			o := dup.Offset(n, tc.r, tc.c)
			if tc.i == o {
				// fmt.Printf("Pass: n=%d, r=%d, c=%d, i=%d\n", n, tc.r, tc.c, tc.i)
			} else {
				t.Fatal(fmt.Sprintf("Fail: n=%d, r=%d, c=%d; Expected i=%d, got i=%d\n", n, tc.r, tc.c, tc.i, o))
			}
		}
	}

}

func TestSplitBaseFilename(t *testing.T) {
	type splitResult struct {
		name string
		c    int
		ext  string
	}
	tt := map[string]splitResult{
		"":                                         {"", 0, ""},
		"flowers.jpg":                              {"flowers", 0, ".jpg"},
		"flowers (0).jpg":                          {"flowers", 1, ".jpg"}, // special case
		"flowers (1).jpg":                          {"flowers", 1, ".jpg"},
		"flowers (2).jpg":                          {"flowers", 2, ".jpg"},
		"flowers (3).jpg":                          {"flowers", 3, ".jpg"},
		"flowers).jpg":                             {"flowers)", 0, ".jpg"},
		"flowers().jpg":                            {"flowers()", 0, ".jpg"},
		"flowers(1).jpg":                           {"flowers(1)", 0, ".jpg"},
		"flowers - Copy.jpg":                       {"flowers", 1, ".jpg"},
		"flowers - Copy (1).jpg":                   {"flowers", 2, ".jpg"},             //special case
		"flowers - Copy (-1).jpg":                  {"flowers - Copy (-1)", 0, ".jpg"}, //special case
		"flowers - Copy (-2).jpg":                  {"flowers - Copy (-2)", 0, ".jpg"}, //special case
		"flowers - Copy (2).jpg":                   {"flowers", 2, ".jpg"},
		"flowers - Copy (3).jpg":                   {"flowers", 3, ".jpg"},
		"flowers - Copy (3) - Copy.jpg":            {"flowers", 4, ".jpg"},
		"flowers - Copy (4) - Copy.jpg":            {"flowers", 5, ".jpg"},
		"flowers - Copy (4) - Copy - Copy.jpg":     {"flowers", 6, ".jpg"},
		"flowers (2) - Copy (4) - Copy - Copy.jpg": {"flowers", 8, ".jpg"},
		".env":        {"", 0, ".env"},
		"env":         {"env", 0, ""},
		"env.":        {"env", 0, "."},
		" - Copy.env": {"", 1, ".env"},
		"- Copy.env":  {"- Copy", 0, ".env"}, // special case, a copied windows file would have the leading space so this is fine
		" (1).foo":    {"", 1, ".foo"},
		" (1)":        {" (1)", 0, ""},
		" - Copy":     {" - Copy", 0, ""},
	}

	for originalName, tc := range tt {
		var r splitResult
		r.name, r.c, r.ext = dup.SplitFileBaseName(originalName)
		if !compareSplit(tc, r) {
			t.Errorf("%q: expected: %+v; got %+v\n", originalName, tc, r)
		}
	}
}
func compareSplit(l1, l2 struct {
	name string
	c    int
	ext  string
}) bool {
	if l1.name != l2.name {
		return false
	}
	if l1.c != l2.c {
		return false
	}
	if l1.ext != l2.ext {
		return false
	}
	return true
}
