// Package dylib is for testing the cgo integration. This file exists solely to
// make sure we don't rewrite Go files that don't import "C" and avoid sending
// duplicate files (both the originals and any cgo-generated ones) to various
// tools and GoArchiveData.
package dylib
