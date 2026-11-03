package users

import "sync/atomic"

// counter is a minimal monotonically increasing metric exported by malgaryd's metrics handler.
type counter struct{ v atomic.Int64 }

func (c *counter) Inc()         { c.v.Add(1) }
func (c *counter) Value() int64 { return c.v.Load() }

// seatLimitHit backs tenant_seat_limit_hit_total (FR-016: operator alerted).
var seatLimitHit counter

// SeatLimitHitTotal is read by the metrics endpoint.
func SeatLimitHitTotal() int64 { return seatLimitHit.Value() }
