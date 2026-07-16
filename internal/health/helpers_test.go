package health

import "go.uber.org/zap"

// noopLogger returns a zap logger that discards all output.
// Used only in tests to avoid nil pointer panics.
func noopLogger() *zap.Logger {
	return zap.NewNop()
}
