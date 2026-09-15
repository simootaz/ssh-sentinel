package dbtest

import (
	"testing"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
)

// The in-memory store has to pass the same suite as the PostgreSQL store:
// the handler and rules tests rely on it behaving like production.
func TestMemoryConformance(t *testing.T) {
	Conformance(t, func(t *testing.T) db.Store { return New() })
}
