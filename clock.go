package agent

import "webtyp.com/time"

// MachineClock is the clock and timezone of the machine the agent runs on: in a browser, the
// user's own computer. It is the default when Config.Clock is nil.
type MachineClock struct{}

func (MachineClock) Now() time.Nano { return time.NowNano() }

// UTCOffsetMinutes is the machine zone's offset from UTC now, in minutes.
func (MachineClock) UTCOffsetMinutes() int { return time.MachineZone().Offset(time.NowUnix()) / 60 }
