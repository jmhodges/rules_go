package gopackages_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bazelbuild/rules_go/go/tools/bazel_testing"
	"golang.org/x/tools/go/packages"
)

// This is to get the platform-specific path to gopackagesdriver in the
// encompassing rules_go's bazel-bin. See the gopackages_test target in
// BUILD.bazel.
var goPkgDriverPath = flag.String("goPkgDriverPath", "", "path to the gopackagesdriver binary")

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

go_library(
	name = "goodbye",
	srcs = ["goodbye.go", "goodbye_other.go"],
	importpath = "fakeimportpath/goodbye",
	visibility = ["//visibility:public"],
)
-- hello.go --
package hello

import "fmt"

func A() string { return fmt.Sprintf("hello is", 12) }
-- goodbye.go --
package goodbye

import "fmt"

func B() string { return fmt.Sprintf("goodbye is", 22) }
-- goodbye_other.go --
package goodbye

import "fmt"

func C() string { return fmt.Sprintf("goodbye is", 45) }
`,
	})
}

// FIXME rename file to gopackagesdriver_test.go
func TestSinglePkgPattern(t *testing.T) {
	// check we can actually build :hello
	if err := bazel_testing.RunBazel("build", "//:hello"); err != nil {
		t.Fatalf("unable to build //:hello normally: %s", err)
	}
	driverPath, err := getDriverPath()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GOPACKAGESDRIVER", driverPath)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, "//:hello")
	if err != nil {
		t.Fatalf("unable to packages.Load: %s", err)
	}
	if len(pkgs) < 1 {
		t.Fatalf("no packages returned")
	}
	if len(pkgs) != 1 {
		t.Errorf("too many packages returned: want 1, got %d", len(pkgs))
	}
	pkg := pkgs[0]
	expectedID := "//:hello"
	if pkg.ID != expectedID {
		t.Errorf("ID: want %#v, got %#v", expectedID, pkg.ID)
	}
	expectedGoFiles := []string{"hello.go"}
	if !compareFiles(expectedGoFiles, pkg.GoFiles) {
		t.Errorf("GoFiles: want (without srcFilePrefix) %v, got %v", expectedGoFiles, pkg.GoFiles)
	}
}

const srcFilePrefix = "/execroot/io_bazel_rules_go/bazel-out/darwin-fastbuild/bin/tests/core/gopackages/darwin_amd64_stripped/gopackages_test.runfiles/io_bazel_rules_go/"

func compareFiles(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i, exp := range expected {
		act := actual[i]
		ind := strings.Index(act, srcFilePrefix)
		if ind == -1 || exp != act[ind+len(srcFilePrefix):] {
			return false
		}
	}
	return true
}

func TestSingleFilePattern(t *testing.T) {
	// check we can actually build :goodbye
	if err := bazel_testing.RunBazel("build", "//:goodbye"); err != nil {
		t.Fatalf("unable to build //:goodbye normally: %s", err)
	}
	driverPath, err := getDriverPath()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GOPACKAGESDRIVER", driverPath)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, "file=./goodbye.go")
	if err != nil {
		t.Fatalf("unable to packages.Load: %s", err)
	}
	if len(pkgs) < 1 {
		t.Fatalf("no packages returned")
	}
	if len(pkgs) != 1 {
		t.Errorf("too many packages returned: want 1, got %d", len(pkgs))
	}
	pkg := pkgs[0]
	expectedID := "//:goodbye"
	if pkg.ID != expectedID {
		t.Errorf("ID: want %#v, got %#v", expectedID, pkg.ID)
	}
	expectedGoFiles := []string{"goodbye.go", "goodbye_other.go"}
	if !compareFiles(expectedGoFiles, pkg.GoFiles) {
		t.Errorf("GoFiles: want (without srcFilePrefix) %v, got %v", expectedGoFiles, pkg.GoFiles)
	}
}

func getDriverPath() (string, error) {
	if *goPkgDriverPath == "" {
		return "", errors.New("-goPkgDriverPath arg was not passed to the test binary")
	}
	driverPath := os.Getenv("PWD") + "/" + *goPkgDriverPath
	f, err := os.Open(driverPath)
	if err != nil {
		return "", fmt.Errorf("gopackagesdriver binary couldn't be opened at -goPkgDriverPath %v: %w", *goPkgDriverPath, err)
	}
	f.Close()
	return driverPath, nil
}
