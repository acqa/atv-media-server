package library

import "testing"

func TestParseMovieName(t *testing.T) {
	cases := []struct {
		in        string
		wantTitle string
		wantYear  int
	}{
		{"The Matrix (1999)", "The Matrix", 1999},
		{"The.Matrix.1999.1080p", "The Matrix", 1999},
		{"Inception [2010]", "Inception", 2010},
		{"Inception", "Inception", 0},
		{"Blade_Runner_1982_1080p", "Blade Runner", 1982},
		{"WALL-E (2008)", "WALL-E", 2008},
		{"", "", 0},
		{"2001 A Space Odyssey", "", 2001}, // ambiguous: leading year wins
		{"Star Wars Episode IV", "Star Wars Episode IV", 0},
		{"Movie.Title.2020.2160p.HEVC", "Movie Title", 2020},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			title, year := ParseMovieName(c.in)
			if title != c.wantTitle || year != c.wantYear {
				t.Errorf("ParseMovieName(%q) = (%q, %d), want (%q, %d)",
					c.in, title, year, c.wantTitle, c.wantYear)
			}
		})
	}
}
