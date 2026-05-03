package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// DedupLabel computes the deterministic Linear label used to
// identify all issues created by the adapter for a given
// Alertmanager alert grouping.
//
// Input: Alertmanager's canonical groupKey (a stable identifier per
// alert grouping — see Alertmanager docs §webhook payload). The
// groupKey is the cartesian product of the route's group_by labels
// for the matched alerts.
//
// Output: "alert:" + first 12 hex chars of sha256(groupKey).
//
// Property: DedupLabel(k1) == DedupLabel(k2) iff k1 == k2.
//
// Collision probability: 1/16^12 ≈ 3.5e-15 per pair. For our
// expected ~250 distinct alert groupings (cardinality bound:
// ~25 SLO rules × ~10 capability combinations) the probability of
// any collision is well under 1e-10 — negligible. Per D2C4AB.7.
//
// Label format:
//   - "alert:" prefix marks adapter-managed dedup labels (humans
//     can filter by this prefix in Linear UI).
//   - 12-char hex suffix is label-safe (no special chars), short
//     enough to keep label lists readable, long enough to avoid
//     practical collisions.
func DedupLabel(groupKey string) string {
	h := sha256.Sum256([]byte(groupKey))
	return "alert:" + hex.EncodeToString(h[:])[:12]
}

// DedupLabelConst is the constant label applied to every
// adapter-created issue, alongside the per-grouping DedupLabel.
// Lets humans filter all adapter-managed issues in Linear UI via
// `label:"alert-managed"`. Per D2C4AB.7.
const DedupLabelConst = "alert-managed"
