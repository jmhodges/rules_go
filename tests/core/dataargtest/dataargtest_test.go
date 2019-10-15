package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/bazelbuild/rules_go/go/tools/bazel_testing"
)

var (
	binaryPath = flag.String("binaryPath", "", "")
	pwd        string
)

func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("unable to get current working directory: %s", err)
	}
	pwd = wd
	bazel_testing.TestMain(m, bazel_testing.Args{
		Main: `
-- BUILD.bazel --
load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
	name = "hello",
	srcs = ["hello.go"],
	importpath = "fakeimportpath/hello",
	visibility = ["//visibility:public"],
)
-- hello.go --
package hello

import "fmt"

func A() string { return fmt.Sprintf("hello is %d", 12) }
`,
	})
}

// Tests that go_bazel_test keeps includes data files correctly and doesn't mess
// up on `args` that include `$(location ...)` calls.
func TestGoldenPath(t *testing.T) {
	f, err := os.Open(filepath.Join(pwd, *binaryPath))
	if err != nil {
		t.Errorf("unable to open md5sum file: %s", err)
	}
	defer f.Close()
}
