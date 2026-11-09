package archdoc

import (
	"reflect"
	"testing"
)

func FuzzDecode(f *testing.F) {
	for _, b := range fixtures(f) {
		f.Add(b)
	}
	f.Add([]byte(`{"schema_version":1,"nodes":[{"id":"a","kind":"api"}]}`))
	f.Add([]byte(`{"schema_version":1,"nodes":[{"id":"a","kind":"client","traffic":null}],"edges":[{}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Decode(data)
		if err != nil {
			return
		}
		enc, err := d.Encode()
		if err != nil {
			t.Fatalf("Encode after successful Decode: %v", err)
		}
		d2, err := Decode(enc)
		if err != nil {
			t.Fatalf("re-decode: %v\n%s", err, enc)
		}
		if !reflect.DeepEqual(d, d2) {
			t.Fatalf("round trip differs\n%+v\n%+v", d, d2)
		}
	})
}
