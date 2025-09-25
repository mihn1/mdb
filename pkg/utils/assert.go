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

func AssertNoErr(err error, msg string) {
	if err != nil {
		panic(err)
	}
}
