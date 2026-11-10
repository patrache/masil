package i18n

import (
	"strings"
	"unicode/utf8"
)

// Particle appends the Korean particle pair ("이/가", "은/는", "을/를",
// "과/와", "으로/로") that fits word's last sound: the first form after a
// final consonant, the second otherwise ("으로/로" also takes 로 after ㄹ).
// Digits use their Korean reading. Anything else gets the bracketed form
// ("Mike을(를)", "Sam(으)로") — guessing English pronunciation is wrong too often.
func Particle(word, pair string) string {
	with, without, ok := strings.Cut(pair, "/")
	if !ok {
		return word + pair
	}
	ro := with == "으로" && without == "로"
	r, _ := utf8.DecodeLastRuneInString(word)
	var batchim, rieul, known bool
	switch {
	case r >= 0xAC00 && r <= 0xD7A3:
		jong := (r - 0xAC00) % 28
		batchim, rieul, known = jong != 0, jong == 8, true
	case r >= '0' && r <= '9':
		// 영 일 이 삼 사 오 육 칠 팔 구
		batchim = strings.ContainsRune("013678", r)
		rieul = strings.ContainsRune("178", r)
		known = true
	}
	switch {
	case !known && ro:
		return word + "(으)로"
	case !known:
		return word + with + "(" + without + ")"
	case ro && (!batchim || rieul):
		return word + without
	case batchim:
		return word + with
	default:
		return word + without
	}
}
