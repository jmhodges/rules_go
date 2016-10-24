package remotecgorepo

/**
#cgo LDFLAGS: -lm
**/
import "C"

func Call() int64 {
	return C.call()
}
