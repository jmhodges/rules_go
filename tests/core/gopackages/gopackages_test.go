package gopackages_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
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
-- hello.go --
package hello

import "fmt"

func A() string { return fmt.Sprintf("A is", 12) }
`,
	})
}

// FIXME rename file to gopackagesdriver_test.go
func Test(t *testing.T) {
	// check we can actually build :hello
	args := []string{
		"build",
		"//:hello",
	}
	if err := bazel_testing.RunBazel(args...); err != nil {
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
	expectedGoFiles := []string{}
	if reflect.DeepEqual(expectedGoFiles, pkg.GoFiles) {
		t.Errorf("GoFiles: want %v, got %v", expectedID, pkg.ID)
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
