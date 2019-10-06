package main

import (
	"flag"
	"os"
	"testing"

	"github.com/bazelbuild/rules_go/go/tools/bazel_testing"
)

var md5Path = flag.String("md5Path", "", "")

func TestMain(m *testing.M) {
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

func TestGoldenPath(t *testing.T) {
	if err := bazel_testing.RunBazel("build", "//:hello"); err != nil {
		t.Fatalf("unable to build //:hello normally: %s", err)
	}
	f, err := os.Open(*md5Path)
	if err != nil {
		t.Errorf("unable to open md5sum file: %s", err)
	}
	f.Close()
}
