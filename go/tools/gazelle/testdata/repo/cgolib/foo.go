package cgolib

import "C"
import "fmt"

import "example.com/repo/lib"

func CCall() int64 {
	// Just for the lib import
	fmt.Println(lib.Answer())
	return C.callC()
}
