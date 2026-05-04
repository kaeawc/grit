package tieredcas

// UploadPolicy decides whether an action result is eligible for promotion
// from a local tier to a remote tier. Cheap, locally-reproducible actions
// shouldn't pay the upload bandwidth cost; expensive, widely-shared ones
// should. Zero-value UploadPolicy denies every upload — callers must
// configure at least one allowed kind or set MinTier/MinResultSize to
// allow uploads.
type UploadPolicy struct {
	// AllowedKinds is the set of action kinds eligible for upload. Empty
	// means "no kinds are allowed" (deny by default). The wildcard "*"
	// matches every kind.
	AllowedKinds []string

	// MinTier is the lowest tier index whose results are allowed to be
	// uploaded. Actions whose results were sourced from a tier with
	// index < MinTier are skipped. The primary local tier is index 0;
	// MinTier=0 promotes everything that satisfies the other gates.
	MinTier int

	// MinResultSize, in bytes, gates the result-size dimension. Results
	// smaller than this are not worth uploading. Zero means no minimum.
	MinResultSize int64
}

// ShouldUpload reports whether an action with the given kind, sourced
// from the given tier, with the given result size in bytes, qualifies
// for remote upload under this policy.
func (p UploadPolicy) ShouldUpload(actionKind string, tier int, resultSize int64) bool {
	if !p.kindAllowed(actionKind) {
		return false
	}
	if tier < p.MinTier {
		return false
	}
	if p.MinResultSize > 0 && resultSize < p.MinResultSize {
		return false
	}
	return true
}

func (p UploadPolicy) kindAllowed(actionKind string) bool {
	for _, k := range p.AllowedKinds {
		if k == "*" || k == actionKind {
			return true
		}
	}
	return false
}
