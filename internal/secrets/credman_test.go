package secrets

import (
	"bytes"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
)

// fakeCred is a credAPI over a map (target => item).
type fakeCred struct {
	items map[string]fakeCredItem
	err   error // returned by every call when set
}

type fakeCredItem struct {
	comment string
	blob    []byte
}

func (f *fakeCred) write(target, comment string, blob []byte) error {
	if f.err != nil {
		return f.err
	}
	if len(blob) > credMaxBlob {
		return errors.New("blob too large")
	}
	f.items[target] = fakeCredItem{comment, bytes.Clone(blob)}
	return nil
}

func (f *fakeCred) read(target string) (string, []byte, error) {
	if f.err != nil {
		return "", nil, f.err
	}
	it, ok := f.items[target]
	if !ok {
		return "", nil, ErrNotFound
	}
	return it.comment, bytes.Clone(it.blob), nil
}

func (f *fakeCred) delete(target string) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.items[target]; !ok {
		return ErrNotFound
	}
	delete(f.items, target)
	return nil
}

func TestCredManChunks(t *testing.T) {
	api := &fakeCred{items: map[string]fakeCredItem{}}
	c := credman{api: api}
	if c.Name() != BackendCredMan {
		t.Fatalf("Name = %q", c.Name())
	}
	acct := "leet/masil.example/U1/cookie"
	value := strings.Repeat("0123456789", 600) // 6,000 bytes
	if err := c.Set(bg, Service, acct, value); err != nil {
		t.Fatal(err)
	}
	if len(api.items) != 4 {
		t.Fatalf("items = %v, want a head + 3 chunks", slices.Sorted(maps.Keys(api.items)))
	}
	head, ok := api.items["masil/"+acct]
	if !ok || string(head.blob) != "3" || head.comment != credChunked {
		t.Fatalf("head = %+v", head)
	}
	total := 0
	for i := range 3 {
		it, ok := api.items["masil/"+acct+"#"+string(rune('0'+i))]
		if !ok || len(it.blob) > 2560 || len(it.blob) == 0 {
			t.Fatalf("chunk %d = %d bytes, %v", i, len(it.blob), ok)
		}
		total += len(it.blob)
	}
	if total != 6000 {
		t.Errorf("chunks hold %d bytes", total)
	}
	got, err := c.Get(bg, Service, acct)
	if err != nil || got != value {
		t.Fatalf("Get = %d bytes, %v", len(got), err)
	}

	// A shorter value replaces it in one item and the stale chunks go.
	if err := c.Set(bg, Service, acct, "short"); err != nil {
		t.Fatal(err)
	}
	if len(api.items) != 1 || string(api.items["masil/"+acct].blob) != "short" || api.items["masil/"+acct].comment != "" {
		t.Fatalf("items = %v", api.items)
	}
	if got, _ := c.Get(bg, Service, acct); got != "short" {
		t.Fatalf("Get = %q", got)
	}

	if err := c.Set(bg, Service, acct, value); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(bg, Service, acct); err != nil {
		t.Fatal(err)
	}
	if len(api.items) != 0 {
		t.Fatalf("Delete left %v", slices.Sorted(maps.Keys(api.items)))
	}
	if _, err := c.Get(bg, Service, acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
	if err := c.Delete(bg, Service, acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete = %v, want ErrNotFound", err)
	}
}

func TestCredManExactLimit(t *testing.T) {
	api := &fakeCred{items: map[string]fakeCredItem{}}
	c := credman{api: api}
	v := strings.Repeat("x", credMaxBlob)
	if err := c.Set(bg, Service, "a/b/c/d", v); err != nil {
		t.Fatal(err)
	}
	if len(api.items) != 1 {
		t.Errorf("a %d-byte value took %d items", credMaxBlob, len(api.items))
	}
	if err := c.Set(bg, Service, "a/b/c/d", strings.Repeat("x", credMaxBlob*credMaxChunks+1)); err == nil {
		t.Error("an oversized value was accepted")
	}
}

func TestCredManUnavailable(t *testing.T) {
	c := credman{api: &fakeCred{items: map[string]fakeCredItem{}, err: ErrUnavailable}}
	if err := c.Set(bg, Service, "a/b/c/d", "v"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Set = %v", err)
	}
	if _, err := c.Get(bg, Service, "a/b/c/d"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Get = %v", err)
	}
}
