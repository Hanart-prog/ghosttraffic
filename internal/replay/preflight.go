package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Hanart-prog/ghosttraffic/internal/egress"
)

// ErrSeedMismatch means the targets did not start from the same state, so a
// comparison between them would be meaningless.
var ErrSeedMismatch = errors.New("replay: targets report different seed state")

// SeedDigests fetches each target's seed digest from /health and fails if they
// differ. A target without one yields an empty digest: equal state is then
// unknown, not assumed, and the caller must record a warning. A target refused
// by egress policy is an error, so nothing is sent to either.
func SeedDigests(ctx context.Context, client *http.Client, baseline, candidate string) (base, cand string, err error) {
	if base, err = seedDigest(ctx, client, baseline); err != nil {
		return "", "", err
	}
	if cand, err = seedDigest(ctx, client, candidate); err != nil {
		return "", "", err
	}
	if base != "" && cand != "" && base != cand {
		return base, cand, fmt.Errorf("%w: baseline %s, candidate %s", ErrSeedMismatch, base, cand)
	}
	return base, cand, nil
}

func seedDigest(ctx context.Context, client *http.Client, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(target, "/")+"/health", nil)
	if err != nil {
		return "", nil
	}
	resp, err := client.Do(req)
	if errors.Is(err, egress.ErrRefused) {
		return "", err
	}
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()
	var health struct {
		SeedDigest string `json:"seed_digest"`
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&health); err != nil {
		return "", nil
	}
	return health.SeedDigest, nil
}
