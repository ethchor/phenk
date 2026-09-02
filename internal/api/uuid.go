package api

import (
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ethchor/phenk/internal/core"
)

// mustAPIUUID converts a core UUID into the generated API type. The generated
// code uses google/uuid's byte array, which is the same sixteen bytes.
func mustAPIUUID(id core.UUID) openapi_types.UUID {
	var out openapi_types.UUID
	copy(out[:], id[:])
	return out
}

// openapiUUID is the generated UUID type, aliased so handler signatures read
// as the interface declares them.
type openapiUUID = openapi_types.UUID

// coreUUID converts back.
func coreUUID(id openapi_types.UUID) core.UUID {
	var out core.UUID
	copy(out[:], id[:])
	return out
}
