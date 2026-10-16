package companion

import (
	"os"
	"testing"

	"vibe/internal/review"
)

// TestMain lets this test binary stand in for vibe as the helper reading
// repositories (see review.ServeHelperIfAsked).
func TestMain(m *testing.M) {
	review.ServeHelperIfAsked()
	os.Exit(m.Run())
}
