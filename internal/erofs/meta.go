package erofs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// metaFile is what a directory of the cache says about the blob next to it.
const metaFile = "meta.json"

// Meta is what was converted, by what, and what the conversion could not do as the archive
// declared it. It is written next to the blob and read again on every later use, so that what a
// verb reports of a layer never depends on that layer being converted in the same run.
//
// The keys are snake case, the shape the verbs of cove report in.
//
//nolint:tagliatelle // The keys are the contract of the cache, written down before the linter's rule.
type Meta struct {
	// BlobSHA256 is the fingerprint of the blob, computed once, when it was written. It is
	// never recomputed here: a blob changed since its conversion still carries the fingerprint
	// of that conversion, and telling the two apart belongs to whoever attaches it.
	BlobSHA256 string `json:"blob_sha256"`
	// WriterVersion is what wrote the blob; see cacheVersion.
	WriterVersion string `json:"writer_version"`
	// ConvertedAt is when it was written.
	ConvertedAt time.Time `json:"converted_at"`
	// SourceDigest names the compressed blob the layer came from.
	SourceDigest string `json:"source_digest,omitempty"`
	// SourceDateEpoch is the date given to everything the archive does not date, the default
	// value included.
	SourceDateEpoch int64 `json:"source_date_epoch"`
	// Entries counts what the layer holds: one per name the archive created.
	Entries int `json:"entries"`
	// NormalizedEntries counts the names bounded to the root, UnknownXattrPrefixes the extended
	// attributes written under a name EROFS does not read in the guest.
	NormalizedEntries    int `json:"normalized_entries"`
	UnknownXattrPrefixes int `json:"unknown_xattr_prefixes"`
	// DiffIDVerified says that the decompressed stream was hashed and matched the diff id the
	// config of the image names. It is false when no diff id was known of, and the blob is then
	// keyed by the digest of the compressed layer instead.
	DiffIDVerified bool `json:"diffid_verified"`
}

// readMeta reads the meta.json of the directory dir.
func readMeta(dir string) (Meta, error) {
	//nolint:gosec // G304: dir is built by the cache from its own root, never from user input.
	data, err := os.ReadFile(filepath.Join(dir, metaFile))
	if err != nil {
		return Meta{}, fmt.Errorf("read %s: %w", metaFile, err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("read %s: %w", metaFile, err)
	}
	return m, nil
}

// writeMeta writes m in the directory dir, which is still the temporary of a conversion: the
// whole directory is moved into the cache afterwards, so nothing here has to survive on its own.
func writeMeta(dir string, m Meta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", metaFile, err)
	}
	if err := os.WriteFile(filepath.Join(dir, metaFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", metaFile, err)
	}
	return nil
}
