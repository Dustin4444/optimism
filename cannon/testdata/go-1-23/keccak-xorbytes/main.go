package main

import (
	"crypto/subtle"
	"fmt"
)

func main() {
	a := make([]byte, 200)
	p := []byte{1, 2, 3}

	subtle.XORBytes(a, a, p)

	fmt.Printf("keccak program. result=%x\n", a)
}
