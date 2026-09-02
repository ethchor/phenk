package web

import "time"

// zeroTime disables the modification-time conditional requests that
// http.ServeContent would otherwise make. Embedded files all report the zero
// time anyway, and cache behaviour is set explicitly by the handler.
var zeroTime time.Time
