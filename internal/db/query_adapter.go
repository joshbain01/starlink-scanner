package db

// QueryAdapter is the narrow read/config interface that CLI commands depend on.
// It exposes only the methods needed by the command layer, keeping commands
// decoupled from the full *DB surface (write methods, maintenance, migrations).
//
// *DB satisfies this interface; the compile-time assertion below enforces that.
type QueryAdapter interface {
	SpatialBuckets(lossThreshold float64) ([]SpatialBucket, error)
	QueryInsights(lossThreshold, snrDelta, noiseDelta float64) ([]InsightEvent, error)
	QueryObstructionMap() ([]ObstructionZone, error)
	QueryReport(p ReportParams) (*Report, error)
	SetConfig(key, value string) error
	GetConfig(key string) (string, error)
}

// Compile-time assertion: *DB must implement QueryAdapter.
var _ QueryAdapter = (*DB)(nil)
