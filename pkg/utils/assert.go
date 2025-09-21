package utils

func AssertMsg(cond bool, msg string) {
	if !cond {
		panic(msg)
	}
}

func Assert(cond bool) {
	if !cond {
		panic("critical invariant violated")
	}
}
