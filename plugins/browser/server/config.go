package server

import (
	"errors"
	"math"

	"google.golang.org/protobuf/types/known/structpb"
)

type config struct {
	MaxFile        int64
	MaxStaging     int64
	MaxAttachments int
	FileSeconds    int
	Width          int
	Height         int
}

func parseConfig(values *structpb.Struct) (config, error) {
	parsed := config{MaxFile: 256 << 20, MaxStaging: 1024 << 20, MaxAttachments: 8, FileSeconds: 600, Width: 1440, Height: 900}
	if values == nil {
		return parsed, nil
	}
	for key, value := range values.Fields {
		var target *int
		minimum, maximum, multiplier := 0, 0, int64(1)
		switch key {
		case "max_file_size_mb":
			minimum, maximum, multiplier = 1, 1024, 1<<20
		case "max_staging_size_mb":
			minimum, maximum, multiplier = 1, 8192, 1<<20
		case "max_attachments":
			target, minimum, maximum = &parsed.MaxAttachments, 1, 64
		case "file_transfer_seconds":
			target, minimum, maximum = &parsed.FileSeconds, 5, 3600
		case "width":
			target, minimum, maximum = &parsed.Width, 640, 4096
		case "height":
			target, minimum, maximum = &parsed.Height, 480, 2160
		case "viewer_max_sessions", "viewer_bootstrap_seconds", "viewer_session_seconds", "viewer_max_file_bytes", "viewer_operation_seconds":
			continue
		default:
			return parsed, errors.New("unknown browser configuration key: " + key)
		}
		number, ok := value.Kind.(*structpb.Value_NumberValue)
		if !ok || math.IsNaN(number.NumberValue) || math.IsInf(number.NumberValue, 0) || math.Trunc(number.NumberValue) != number.NumberValue || number.NumberValue < float64(minimum) || number.NumberValue > float64(maximum) {
			return parsed, errors.New("invalid browser numeric configuration: " + key)
		}
		if target != nil {
			*target = int(number.NumberValue)
		} else if key == "max_file_size_mb" {
			parsed.MaxFile = int64(number.NumberValue) * multiplier
		} else {
			parsed.MaxStaging = int64(number.NumberValue) * multiplier
		}
	}
	if parsed.MaxFile > parsed.MaxStaging {
		return parsed, errors.New("browser staging quota must fit one maximum-sized file")
	}
	return parsed, nil
}
