package starlink

import "context"

// DishDataSource is the interface Collector depends on. *Client satisfies it.
type DishDataSource interface {
	GetStatus(ctx context.Context) (Status, error)
	GetHistory(ctx context.Context) (History, error)
	GetDeviceInfo(ctx context.Context) (DeviceInfo, error)
	Close()
}

// FixtureSource returns canned values and is intended for use in tests.
type FixtureSource struct {
	status  Status
	history History
	info    DeviceInfo
}

// NewFixtureSource constructs a FixtureSource that returns the provided values.
func NewFixtureSource(status Status, history History, info DeviceInfo) *FixtureSource {
	return &FixtureSource{status: status, history: history, info: info}
}

func (f *FixtureSource) GetStatus(_ context.Context) (Status, error)     { return f.status, nil }
func (f *FixtureSource) GetHistory(_ context.Context) (History, error)   { return f.history, nil }
func (f *FixtureSource) GetDeviceInfo(_ context.Context) (DeviceInfo, error) {
	return f.info, nil
}
func (f *FixtureSource) Close() {}
