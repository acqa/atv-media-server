package library

import (
	"sync"
	"testing"
)

func TestLibrary_ReplaceGetCount(t *testing.T) {
	lib := NewLibrary()
	if lib.Count() != 0 {
		t.Fatalf("empty Library should have Count 0, got %d", lib.Count())
	}

	lib.Replace([]Movie{
		{ID: "a", Title: "Alpha", Year: 2000},
		{ID: "b", Title: "Bravo", Year: 1999},
	})
	if lib.Count() != 2 {
		t.Fatalf("Count after Replace: want 2, got %d", lib.Count())
	}

	m, ok := lib.Get("a")
	if !ok || m.Title != "Alpha" {
		t.Errorf("Get(a): want Alpha, got %+v ok=%v", m, ok)
	}
	if _, ok := lib.Get("missing"); ok {
		t.Errorf("Get(missing) should return ok=false")
	}
}

func TestLibrary_AllSortedByTitleThenYear(t *testing.T) {
	lib := NewLibrary()
	lib.Replace([]Movie{
		{ID: "1", Title: "Zorro", Year: 1998},
		{ID: "2", Title: "alpha", Year: 2010}, // lowercase to confirm case-insensitive sort
		{ID: "3", Title: "Alpha", Year: 2000},
		{ID: "4", Title: "Beta", Year: 2005},
	})
	out := lib.All()
	want := []string{"3", "2", "4", "1"} // Alpha(2000), alpha(2010), Beta, Zorro
	if len(out) != len(want) {
		t.Fatalf("All length: want %d, got %d", len(want), len(out))
	}
	for i, id := range want {
		if out[i].ID != id {
			t.Errorf("All[%d].ID = %q, want %q (got order: %v)", i, out[i].ID, id, idsOf(out))
		}
	}
}

func TestLibrary_AllReturnsSnapshot(t *testing.T) {
	lib := NewLibrary()
	lib.Replace([]Movie{{ID: "a", Title: "Alpha"}})
	out := lib.All()
	out[0].Title = "Mutated"
	again, _ := lib.Get("a")
	if again.Title != "Alpha" {
		t.Errorf("Library mutated via snapshot: got %q", again.Title)
	}
}

func TestLibrary_ConcurrentReadWrite(t *testing.T) {
	lib := NewLibrary()
	lib.Replace([]Movie{{ID: "warm", Title: "Warm"}})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = lib.All()
				_, _ = lib.Get("warm")
				_ = lib.Count()
			}
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lib.Replace([]Movie{
				{ID: "warm", Title: "Warm"},
				{ID: "x", Title: "X"},
			})
		}(i)
	}
	wg.Wait()
	// Must not deadlock or race; -race will catch issues.
}

func TestLibrary_ScanIntoIntegration(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, []string{
		"The Matrix (1999)/m.mkv",
		"Inception (2010)/i.mp4",
	})
	lib := NewLibrary()
	if err := lib.ScanInto(dir); err != nil {
		t.Fatalf("ScanInto: %v", err)
	}
	if lib.Count() != 2 {
		t.Fatalf("Count after ScanInto: want 2, got %d", lib.Count())
	}
}

func idsOf(ms []Movie) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}
