package config

import (
	"math"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/structpb"
)

const DefaultMaxAutoSyncSize int64 = 10 << 20
const MaxAutoSyncSize int64 = 1 << 30

type SyncConfig struct {
	Interval        time.Duration
	MaxAutoSyncSize int64
}

func ValidateSyncClient(values *structpb.Struct) error { _, err := ParseSyncClient(values); return err }
func ValidateSyncServer(values *structpb.Struct) error { _, err := ParseSyncServer(values); return err }
func SyncInterval(values *structpb.Struct) (time.Duration, error) {
	c, err := ParseSyncClient(values)
	return c.Interval, err
}
func ParseSyncClient(values *structpb.Struct) (SyncConfig, error) { return parseSync(values, true) }
func ParseSyncServer(values *structpb.Struct) (SyncConfig, error) { return parseSync(values, false) }

func parseSync(values *structpb.Struct, client bool) (SyncConfig, error) {
	c := SyncConfig{Interval: 2 * time.Second, MaxAutoSyncSize: DefaultMaxAutoSyncSize}
	for key, value := range values.GetFields() {
		if key != "max_auto_sync_size" && (key != "scan_interval_ms" || !client) {
			return c, connect.NewError(connect.CodeInvalidArgument, "UNKNOWN_SYNC_CONFIG_FIELD")
		}
		number, ok := value.GetKind().(*structpb.Value_NumberValue)
		if !ok || math.IsNaN(number.NumberValue) || math.IsInf(number.NumberValue, 0) || math.Trunc(number.NumberValue) != number.NumberValue {
			return c, connect.NewError(connect.CodeInvalidArgument, "INVALID_SYNC_CONFIG_VALUE")
		}
		switch key {
		case "scan_interval_ms":
			if number.NumberValue < 100 || number.NumberValue > 60000 {
				return c, connect.NewError(connect.CodeInvalidArgument, "INVALID_SCAN_INTERVAL")
			}
			c.Interval = time.Duration(number.NumberValue) * time.Millisecond
		case "max_auto_sync_size":
			if number.NumberValue < 1 || number.NumberValue > float64(MaxAutoSyncSize) {
				return c, connect.NewError(connect.CodeInvalidArgument, "INVALID_MAX_AUTO_SYNC_SIZE")
			}
			c.MaxAutoSyncSize = int64(number.NumberValue)
		}
	}
	return c, nil
}
