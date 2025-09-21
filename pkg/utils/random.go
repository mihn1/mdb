package utils

import "math/rand"

func RandomBool() bool {
	return RandomInt(0, 2) == 0
}

func RandomInt(i1, i2 int) int {
	return i1 + rand.Intn(i2-i1)
}
