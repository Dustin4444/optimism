package main

import (
	"fmt"

	"golang.org/x/crypto/sha3"
)

func main() {

	var result []byte
	state := sha3.NewLegacyKeccak256()
	state.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9})
	result = state.Sum(result)

	fmt.Printf("keccak program. result=%x\n", result)
}
