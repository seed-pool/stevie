package livetv

import "testing"

func TestCategoryNameLess_LettersBeforeNumbers(t *testing.T) {
	names := []string{"10 Top", "Action", "⁴K Movies", "Drama", "123", "Comedy"}
	SortNamed(names)
	want := []string{"Action", "Comedy", "Drama", "10 Top", "123", "⁴K Movies"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v want %v", names, want)
		}
	}
}
