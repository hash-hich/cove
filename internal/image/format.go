package image

import (
	"encoding/json"
	"io"
)

// WriteJSON writes r to w as one JSON object, the contract of pull --json: what was asked for,
// what was pulled and what it took.
//
// A write error is ignored here as everywhere cove writes to its own streams: a closed pipe is
// not a failure of the command.
func WriteJSON(w io.Writer, r Result) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resultJSON{
		Ref:           r.Ref,
		Digest:        r.Digest.String(),
		Platform:      r.Platform.String(),
		LayersTotal:   r.LayersTotal,
		LayersFetched: r.LayersFetched,
		Bytes:         r.Bytes,
		Cached:        r.Cached,
		Entries:       r.Entries,
		Normalized:    r.Unpacked.NormalizedEntries,
		UnknownXattrs: r.Unpacked.UnknownXattrPrefixes,
	})
}

// resultJSON is a result as pull reports it. The keys are those the spec of the verb names, in
// snake case.
//
//nolint:tagliatelle // The keys are the contract of the verb, written down before the linter's rule.
type resultJSON struct {
	Ref           string `json:"ref"`
	Digest        string `json:"digest"`
	Platform      string `json:"platform"`
	LayersTotal   int    `json:"layers_total"`
	LayersFetched int    `json:"layers_fetched"`
	Bytes         int64  `json:"bytes"`
	Cached        bool   `json:"cached"`
	Entries       int    `json:"entries"`
	Normalized    int    `json:"normalized_entries"`
	UnknownXattrs int    `json:"unknown_xattr_prefixes"`
}
