package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrSeedMismatch means the targets did not start from the same state, so a
// comparison between them would be meaningless.
var ErrSeedMismatch = errors.New("replay: targets report different seed state")

// SeedDigests fetches each target's seed digest from /health and fails if they
// differ. A target without one yields an empty digest: equal state is then
// unknown, not assumed, and the caller must record a warning.
func SeedDigests(ctx context.Context, client *http.Client, baseline, candidate string) (base, cand string, err error) {
	base = seedDigest(ctx, client, baseline)
	cand = seedDigest(ctx, client, candidate)
	if base != "" && cand != "" && base != cand {
		return base, cand, fmt.Errorf("%w: baseline %s, candidate %s", ErrSeedMismatch, base, cand)
	}
	return base, cand, nil
}

func seedDigest(ctx context.Context, client *http.Client, target string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(target, "/")+"/health", nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var health struct {
		SeedDigest string `json:"seed_digest"`
	}
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&health); err != nil {
		return ""
	}
	return health.SeedDigest
}
