package main

import (
	"golang.org/x/sys/unix"
)

func GetUserSystemTimes(duration *Duration) {
	var tms unix.Tms
	ticks, err := unix.Times(&tms)
	check(err)
	duration.User = time.Duration(tms.CUtime) * time.Millisecond * 10
	duration.System = time.Duration(tms.CStime) * time.Millisecond * 10
}
