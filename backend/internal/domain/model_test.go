package domain

import "testing"

func TestCompetitionRanks(t *testing.T) {
	r := []Row{{Score: 200}, {Score: 200}, {Score: 100}, {Score: 0}, {Score: 0}}
	Rank(r)
	for i, want := range []int{1, 1, 3, 4, 4} {
		if r[i].Rank != want {
			t.Fatalf("row %d got rank %d want %d", i, r[i].Rank, want)
		}
	}
}
func TestNames(t *testing.T) {
	for _, s := range []string{"", "  ", "a\nb", "\u202enames"} {
		if _, err := Name(s); err == nil {
			t.Fatalf("accepted invalid name %q", s)
		}
	}
	if got, err := Name("  <script>  "); err != nil || got != "<script>" {
		t.Fatal(got, err)
	}
}
