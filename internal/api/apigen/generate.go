// Package apigen holds the code generated from api/openapi.yaml.
//
// Nothing here is written by hand. Run `make generate` after changing the
// specification; CI fails if the checked-in output is stale, so the API and its
// documentation cannot drift apart.
package apigen

//go:generate go tool oapi-codegen -config config.yaml ../../../api/openapi.yaml
