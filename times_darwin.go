package main

/*
#include <sys/times.h>
#include <time.h>
*/
import "C"
import "time"

type Tms struct {
	Utime  int64
	Stime  int64
	CUtime int64
	CStime int64
}

func GetUserSystemTimes(duration *Duration) {
	var cTms C.struct_tms
	C.times(&cTms)
	tms := Tms{
		Utime:  int64(cTms.tms_utime),
		Stime:  int64(cTms.tms_stime),
		CUtime: int64(cTms.tms_cutime),
		CStime: int64(cTms.tms_cstime),
	}
	duration.User = time.Duration(tms.CUtime) * time.Millisecond * 10
	duration.System = time.Duration(tms.CStime) * time.Millisecond * 10
}
