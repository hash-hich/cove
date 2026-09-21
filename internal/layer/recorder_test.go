package layer_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/layer"
)

// recorder is the Writer of the tests: each call becomes one line, what a writer would have been
// told, in order. The attributes and the content are kept aside for the tests that read them, and
// refuse names the path whose write fails.
type recorder struct {
	calls   []string
	attrs   map[string]layer.Attr
	content map[string]string
	refuse  string
}

// record keeps the line of one call, with attr under p when the call carries attributes.
func (r *recorder) record(p string, attr *layer.Attr, format string, args ...any) error {
	if p == r.refuse {
		return errors.New("the writer refuses it")
	}
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
	if attr != nil {
		if r.attrs == nil {
			r.attrs = map[string]layer.Attr{}
		}
		r.attrs[p] = *attr
	}
	return nil
}

// owner writes the mode and the owner of attr as the lines of the recorder carry them.
func owner(attr layer.Attr) string {
	return fmt.Sprintf("%s %d:%d", attr.Mode, attr.UID, attr.GID)
}

func (r *recorder) Mkdir(p string, attr layer.Attr) error {
	return r.record(p, &attr, "mkdir %s %s", p, owner(attr))
}

func (r *recorder) Setattr(p string, attr layer.Attr) error {
	return r.record(p, &attr, "setattr %s %s", p, owner(attr))
}

func (r *recorder) WriteFile(p string, attr layer.Attr, size int64, content io.Reader) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("read the content: %w", err)
	}
	if r.content == nil {
		r.content = map[string]string{}
	}
	r.content[p] = string(data)
	return r.record(p, &attr, "file %s %s %d", p, owner(attr), size)
}

func (r *recorder) Symlink(p string, attr layer.Attr, target string) error {
	return r.record(p, &attr, "symlink %s %s %s", p, owner(attr), target)
}

func (r *recorder) Link(p, target string) error {
	return r.record(p, nil, "link %s %s", p, target)
}

func (r *recorder) Mknod(p string, attr layer.Attr, major, minor int64) error {
	return r.record(p, &attr, "mknod %s %s %d,%d", p, owner(attr), major, minor)
}

func (r *recorder) Setxattr(p, name, value string) error {
	return r.record(p, nil, "setxattr %s %s=%q", p, name, value)
}

func (r *recorder) RemoveAll(p string) error {
	return r.record(p, nil, "removeall %s", p)
}

// applyAll unpacks the archive of entries and returns what the writer was told, what was
// counted and what was said.
func applyAll(t *testing.T, entries ...entry) (*recorder, layer.Counts, string) {
	t.Helper()
	rec := &recorder{}
	var log bytes.Buffer
	counts, err := layer.Apply(id, archive(t, entries...), rec, &log)
	require.NoError(t, err)
	return rec, counts, log.String()
}

// refused unpacks the archive of entries and returns the error that refused it, which must name
// the layer and the entry.
func refused(t *testing.T, entry string, entries ...entry) *layer.EntryError {
	t.Helper()
	_, err := layer.Apply(id, archive(t, entries...), &recorder{}, io.Discard)
	entryErr, ok := errors.AsType[*layer.EntryError](err)
	require.True(t, ok, "%v", err)
	require.Equal(t, id, entryErr.Layer)
	require.Equal(t, entry, entryErr.Entry)
	require.Contains(t, err.Error(), "layer "+id+": entry "+fmt.Sprintf("%q", entry)+": ")
	return entryErr
}

// partial is a writer that takes one byte of a content and leaves the rest.
type partial struct {
	recorder
}

func (*partial) WriteFile(_ string, _ layer.Attr, _ int64, content io.Reader) error {
	if _, err := content.Read(make([]byte, 1)); err != nil {
		return fmt.Errorf("read one byte: %w", err)
	}
	return nil
}
