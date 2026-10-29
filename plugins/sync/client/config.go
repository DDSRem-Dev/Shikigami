package client

import (
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	"google.golang.org/protobuf/types/known/structpb"
)

func ValidateConfig(values *structpb.Struct) error { return schema.ValidateSyncClient(values) }
