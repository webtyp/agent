package agent

import "webtyp.com/time"

// MachineClock is the clock and timezone of the machine the agent runs on: in a browser, the
// user's own computer. It is the default when Config.Clock is nil.
type MachineClock struct{}

func (MachineClock) Now() int64 { return time.Now() }

// UTCOffsetMinutes is the machine's offset. webtyp.com/time detects it in whole hours.
func (MachineClock) UTCOffsetMinutes() int { return time.GetTimeZoneOffset() * 60 }
