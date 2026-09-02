package core

import "time"

// nowMillis is split out so tests can reason about UUID v7 ordering without
// reaching for the wall clock.
func nowMillis() int64 { return time.Now().UnixMilli() }
