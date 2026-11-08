package dnsprovider

import (
	"context"
	"reflect"
	"testing"
)

// fakeSet emulates an RRset style vendor that replaces the whole set at once.
type fakeSet struct {
	values []string
	calls  int
}

func (f *fakeSet) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	out := make([]Record, 0, len(f.values))
	for _, v := range f.values {
		out = append(out, Record{RecordID: normSub(sub), Value: v})
	}
	return out, nil
}

func (f *fakeSet) ReplaceA(ctx context.Context, root, sub string, values []string, ttl int) error {
	f.values = append([]string{}, values...)
	f.calls++
	return nil
}

func (f *fakeSet) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	f.values = append(f.values, value)
	return Record{Value: value}, nil
}
func (f *fakeSet) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	return nil
}
func (f *fakeSet) Delete(ctx context.Context, root, recordID string) error { return nil }
func (f *fakeSet) Verify(ctx context.Context, root string) error           { return nil }

func TestSyncSetCreatesSlots(t *testing.T) {
	f := &fakeSet{}
	res, err := Sync(context.Background(), f, SyncOptions{
		RootDomain: "example.com", SubDomain: "cdn", Slots: 2, TTL: 600,
		Desired: []string{"1.1.1.1", "2.2.2.2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.values, []string{"1.1.1.1", "2.2.2.2"}) {
		t.Fatalf("unexpected set: %v", f.values)
	}
	if len(res.Managed) != 2 {
		t.Fatalf("expected 2 managed rows, got %d", len(res.Managed))
	}
}

func TestSyncSetRollingSwapsOne(t *testing.T) {
	f := &fakeSet{values: []string{"1.1.1.1", "2.2.2.2"}}
	_, err := Sync(context.Background(), f, SyncOptions{
		RootDomain: "example.com", SubDomain: "cdn", Slots: 2, TTL: 600,
		Desired: []string{"3.3.3.3", "4.4.4.4"}, Rolling: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.values, []string{"3.3.3.3", "2.2.2.2"}) {
		t.Fatalf("rolling should swap exactly one slot, got %v", f.values)
	}
}

func TestSyncSetFullReplace(t *testing.T) {
	f := &fakeSet{values: []string{"1.1.1.1", "2.2.2.2"}}
	_, err := Sync(context.Background(), f, SyncOptions{
		RootDomain: "example.com", SubDomain: "cdn", Slots: 2, TTL: 600,
		Desired: []string{"3.3.3.3", "4.4.4.4"}, Rolling: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.values, []string{"3.3.3.3", "4.4.4.4"}) {
		t.Fatalf("expected full replace, got %v", f.values)
	}
}

func TestSyncSetNoopWhenEqual(t *testing.T) {
	f := &fakeSet{values: []string{"1.1.1.1", "2.2.2.2"}}
	res, err := Sync(context.Background(), f, SyncOptions{
		RootDomain: "example.com", SubDomain: "cdn", Slots: 2, TTL: 600,
		Desired: []string{"1.1.1.1", "2.2.2.2"}, Rolling: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatalf("expected no write, got %d calls", f.calls)
	}
	if len(res.Changes) != 0 {
		t.Fatalf("expected no changes, got %v", res.Changes)
	}
}

func TestRegistryHasProviders(t *testing.T) {
	list := List()
	if len(list) < 10 {
		t.Fatalf("expected a broad provider list, got %d", len(list))
	}
	for _, d := range list {
		if d.Name == "" || d.Label == "" {
			t.Fatalf("provider missing metadata: %+v", d)
		}
		if len(d.Fields) == 0 {
			t.Fatalf("provider %s has no credential fields", d.Name)
		}
		if d.MaxSlots < 1 || d.MaxSlots > 8 {
			t.Fatalf("provider %s has invalid MaxSlots %d", d.Name, d.MaxSlots)
		}
	}
}

// fakeLines records the line string the sync layer hands to the vendor, so a
// refactor cannot silently drop carrier-level routing.
type fakeLines struct {
	line    string
	created []string
}

func (f *fakeLines) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	f.line = line
	return nil, nil
}
func (f *fakeLines) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	f.line = line
	f.created = append(f.created, value)
	return Record{RecordID: "1", Value: value, Line: line}, nil
}
func (f *fakeLines) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	f.line = line
	return nil
}
func (f *fakeLines) Delete(ctx context.Context, root, recordID string) error { return nil }
func (f *fakeLines) Verify(ctx context.Context, root string) error           { return nil }

func TestSyncForwardsRecordLine(t *testing.T) {
	f := &fakeLines{}
	_, err := Sync(context.Background(), f, SyncOptions{
		RootDomain: "example.com", SubDomain: "cdn", RecordLine: "电信",
		Slots: 1, TTL: 600, Desired: []string{"1.1.1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.line != "电信" {
		t.Fatalf("sync must forward the line untouched, got %q", f.line)
	}
	if len(f.created) != 1 || f.created[0] != "1.1.1.1" {
		t.Fatalf("expected one created record, got %v", f.created)
	}
}
