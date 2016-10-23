package cgolib

import (
	"fmt"

	"example.com/repo/lib"
	"example.com/repo/lib/deep"
)

func PureCall() int64 {
	// just for the extra import that's not in the CgoFiles
	var d deep.Thought
	fmt.Println(d.Compute())
	return lib.Answer()
}
