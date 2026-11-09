package i18n_test

import (
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

func TestParticle(t *testing.T) {
	cases := []struct{ word, pair, want string }{
		{"노바 개발방", "은/는", "노바 개발방은"},
		{"하나", "은/는", "하나는"},
		{"서울", "으로/로", "서울로"},
		{"부산", "으로/로", "부산으로"},
		{"바다", "으로/로", "바다로"},
		{"3", "이/가", "3이"},
		{"2", "이/가", "2가"},
		{"10", "을/를", "10을"},
		{"7", "으로/로", "7로"},
		{"6", "으로/로", "6으로"},
		{"민준", "과/와", "민준과"},
		{"하나", "과/와", "하나와"},
		{"Mike", "을/를", "Mike을(를)"},
		{"Sam", "이/가", "Sam이(가)"},
		{"Sam", "으로/로", "Sam(으)로"},
		{"", "은/는", "은(는)"},
	}
	for _, c := range cases {
		if got := i18n.Particle(c.word, c.pair); got != c.want {
			t.Errorf("Particle(%q,%q) = %q, want %q", c.word, c.pair, got, c.want)
		}
	}
}
