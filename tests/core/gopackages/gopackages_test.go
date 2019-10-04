package gopackages_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bazelbuild/rules_go/go/tools/bazel" // FIXME remove
	"github.com/bazelbuild/rules_go/go/tools/bazel_testing"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/go/packages"
)

// This is to get the platform-specific path to gopackagesdriver in the
// encompassing rules_go's bazel-bin. See the gopackages_test target in
// BUILD.bazel.
var goPkgDriverPath = flag.String("goPkgDriverPath", "", "path to the gopackagesdriver binary")

// FIXME should this test directory be somewhere else?
func TestMain(m *testing.M) {
	bazel_testing.TestMain(m, bazel_testing.Args{
		Nogo: "@//:gopackagesdriver_nogo",
		Main: `
-- BUILD.bazel --
load("@io_bazel_rules_go//go:def.bzl", "go_library", "nogo")

nogo(
    name = "gopackagesdriver_nogo",
    vet = True,
    visibility = ["//visibility:public"],
)

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

go_library(
	name = "hascgo",
	srcs = ["hascgo.go"],
	importpath = "fakeimportpath/hascgo",
	visibility = ["//visibility:public"],
	cgo = True,
)

-- hello.go --
package hello

import "fmt"

func A() string { return fmt.Sprintf("hello is %d", 12) }
-- goodbye.go --
package goodbye

import "fmt"

func B() string { return fmt.Sprintf("goodbye is %d", 22) }
-- goodbye_other.go --
package goodbye

import "fmt"

func C() string { return fmt.Sprintf("goodbye is %d", 45) }

-- hascgo.go --
package hascgo

// int foo = 12;
import "C"

var foo = int(C.foo)
`,
	})
}

// FIXME rename file to gopackagesdriver_test.go
func TestSinglePkgPattern(t *testing.T) {
	rf, rerr := bazel.ListRunfiles()
	if rerr != nil {
		t.Fatalf("FIXME ListRunfiles failed: %s", rerr)
	} else {
		t.Logf("FIXME wtf %s", rf)
	}

	foo, err := bazel_testing.BazelOutput("info")
	if err != nil {
		t.Fatalf("FIXME bazel info failed with: %s", err)
	} else {
		t.Logf("FIXME oh we got something: %s", foo)
	}
	t.Logf("FIXME hrm we are in PWD %#v", os.Getenv("PWD"))
	// check we can actually build :hello
	if err := bazel_testing.RunBazel("build", "//:hello"); err != nil {
		t.Fatalf("unable to build //:hello normally: %s", err)
	}
	driverPath, err := getDriverPath()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GOPACKAGESDRIVER", driverPath) // FIXME Use Env and os.Environ
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	expectedImportPath := "fakeimportpath/hello"
	if expectedImportPath != pkg.PkgPath {
		t.Errorf("PkgPath: want %#v, got %#v", expectedImportPath, pkg.PkgPath)
	}
	expectedGoFiles := []string{"hello.go"}
	if !compareFiles(expectedGoFiles, pkg.GoFiles) {
		t.Errorf("GoFiles: want (without srcFilePrefix) %v, got %v", expectedGoFiles, pkg.GoFiles)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles,
		Context: ctx,
		Logf:    t.Logf,
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
	expectedImportPath := "fakeimportpath/goodbye"
	if expectedImportPath != pkg.PkgPath {
		t.Errorf("PkgPath: want %#v, got %#v", expectedImportPath, pkg.PkgPath)
	}
	expectedGoFiles := []string{"goodbye.go", "goodbye_other.go"}
	if !compareFiles(expectedGoFiles, pkg.GoFiles) {
		t.Errorf("GoFiles: want (without srcFilePrefix) %v, got %v", expectedGoFiles, pkg.GoFiles)
	}

	// FIXME Testing for absolute files doesn't seem to work because we can't do
	// the environ work done in BazelCmd in the bazel commands inside
	// gopackagesdriver. Will need to talk to jayconrod et. al. about this.

	absPath, err := filepath.Abs("./goodbye_other.go")
	if err != nil {
		t.Fatalf("unable to get goodbye_other.go's absolute file path")
	}
	pkgs, err = packages.Load(cfg, fmt.Sprintf("file=%s", absPath))
	if err != nil {
		t.Fatalf("unable to packages.Load: %s", err)
	}
	if len(pkgs) < 1 {
		t.Fatalf("no packages returned")
	}
	if len(pkgs) != 1 {
		t.Errorf("too many packages returned: want 1, got %d", len(pkgs))
	}
	if pkg.ID != expectedID {
		t.Errorf("absolute path, ID: want %#v, got %#v", expectedID, pkg.ID)
	}
	if expectedImportPath != pkg.PkgPath {
		t.Errorf("abolute path, PkgPath: want %#v, got %#v", expectedImportPath, pkg.PkgPath)
	}
	if !compareFiles(expectedGoFiles, pkg.GoFiles) {
		t.Errorf("absolute path, GoFiles: want (without srcFilePrefix) %v, got %v", expectedGoFiles, pkg.GoFiles)
	}
}

func TestCompiledGoFilesIncludesCgo(t *testing.T) {
	// FIXME get those cgo intermediate files from somewhere
	t.Skipf("ask about where to find to find or how to cgo generated intermediate files")
	// check we can actually build :goodbye
	if err := bazel_testing.RunBazel("build", "//:hascgo"); err != nil {
		t.Fatalf("unable to build //:hascgo normally: %s", err)
	}

	driverPath, err := getDriverPath()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GOPACKAGESDRIVER", driverPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg := &packages.Config{
		Mode:    packages.NeedCompiledGoFiles,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, "//:hascgo")
	if len(pkgs) < 1 {
		t.Fatalf("no packages returned")
	}
	if len(pkgs) != 1 {
		t.Errorf("too many packages returned: want 1, got %d", len(pkgs))
	}
	pkg := pkgs[0]
	expectedID := "//:hascgo"
	if pkg.ID != expectedID {
		t.Errorf("ID: want %#v, got %#v", expectedID, pkg.ID)
	}
	expectedImportPath := "fakeimportpath/hascgo"
	if expectedImportPath != pkg.PkgPath {
		t.Errorf("PkgPath: want %#v, got %#v", expectedImportPath, pkg.PkgPath)
	}
	expectedCompiledGoFiles := []string{"FIXME foobar"}
	if !compareFiles(expectedCompiledGoFiles, pkg.CompiledGoFiles) {
		t.Errorf("CompiledGoFiles: want (without srcFilePrefix) %v, got %v", expectedCompiledGoFiles, pkg.CompiledGoFiles)
	}
}

func TestWithDepsInFilesAndExportAspects(t *testing.T) {
	t.Skipf("doesn't do deps, yet") // FIXME deps!
}

func TestExportedTypeCheckData(t *testing.T) {
	// FIXME exported type check information!
	if err := bazel_testing.RunBazel("build", "//:hello"); err != nil {
		t.Fatalf("unable to build //:hello normally: %s", err)
	}
	driverPath, err := getDriverPath()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GOPACKAGESDRIVER", driverPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg := &packages.Config{
		Mode:    packages.NeedExportsFile,
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
	expectedExportFile := "hello.a"
	if compareFile(expectedExportFile, pkg.ExportFile) {
		t.Errorf("ExportFile: want %#v, got %#v", expectedExportFile, pkg.ExportFile)
	}
	// FIXME test type check info from this and test cgo version.
}

func TestMultiplePatterns(t *testing.T) {
	t.Skipf("doesn't do multiple patterns, yet") // FIXME multiple patterns!
}

func TestStdlib(t *testing.T) {
	testcases := []struct {
		inputPatterns []string
		mode          packages.LoadMode
		outputPkgs    []*packages.Package
	}{
		{
			[]string{"builtin"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				&packages.Package{
					ID:      "@go_sdk//stdlibstub:builtin",
					Name:    "builtin",
					PkgPath: "builtin",
					GoFiles: []string{"external/go_sdk/src/builtin/builtin.go"},
				},
			},
		},
		{
			[]string{"@go_sdk//stdlibstub:builtin"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				&packages.Package{
					ID:      "@go_sdk//stdlibstub:builtin",
					Name:    "builtin",
					PkgPath: "builtin",
					GoFiles: []string{"external/go_sdk/src/builtin/builtin.go"},
				},
			},
		},
	}

	// FIXME delete
	driverPath, err := getDriverPath()
	if err != nil {
		t.Fatal(err)
	}
	// FIXME using Config.Env doesn't work because gopackagesdriver isn't found
	// in the interior bazel run.
	for tcInd, tc := range testcases {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cfg := &packages.Config{
			Mode:    tc.mode,
			Context: ctx,
			Env:     append(os.Environ(), fmt.Sprintf("GOPACKAGESDRIVER=%s", driverPath)),
		}
		t.Logf("FIXME inputpatterns: %q", tc.inputPatterns)
		pkgs, err := packages.Load(cfg, tc.inputPatterns...)
		if err != nil {
			t.Errorf("testcase#%d Load: %s", tcInd, err)
			continue
		}
		if len(tc.outputPkgs) != len(pkgs) {
			t.Errorf("testcase#%d num pkgs: want %d pkgs, got %d pkgs (want %q, got %q)", tcInd, len(tc.outputPkgs), len(pkgs), tc.outputPkgs, pkgs)
		} else {
			for i, exp := range tc.outputPkgs {
				opt := cmp.FilterPath(
					func(p cmp.Path) bool {
						switch p.Last().String() {
						case ".GoFiles", ".CompiledGoFiles", ".OtherFiles":
							return true
						}
						return false
					},
					cmp.Transformer("RelativizeFilePath", func(absPaths []string) []string {
						out := make([]string, len(absPaths))
						for i, absPath := range absPaths {
							if absPath == "" {
								out[i] = ""
								continue
							}
							ind := strings.Index(absPath, srcFilePrefix)
							if ind == -1 {
								out[i] = absPath
								continue
							}
							out[i] = absPath[ind+len(srcFilePrefix):]
						}
						return out
					}),
				)
				if !cmp.Equal(exp, pkgs[i], opt) {
					t.Errorf("testcase#%d package %d, diff: %s", tcInd, i, cmp.Diff(exp, pkgs[i]))
				}
			}
		}
	}
}

// FIXME add to init or something
//
// FIXME unfortunately, we have to use the querytool.sh directly instead of
// getting because bazel_testing.Main doesn't seem to copy over the BUILD file
// for go/tools/gopackagesdriver-nonshim for reasons I dno't yet understand.
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

// FIXME move func below tests?
func compareFiles(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i, exp := range expected {
		if !compareFile(exp, actual[i]) {
			return false
		}
	}
	return true
}

const srcFilePrefix = "bazel_testing/bazel_go_test/main/"

func compareFile(expected, actual string) bool {
	pwd, err := os.Getwd()
	if err != nil {
		return false // FIXME maybe don't do this?
	}
	return filepath.Join(pwd, expected) == actual
	// ind := strings.Index(actual, srcFilePrefix)
	// if ind == -1 || expected != actual[ind+len(srcFilePrefix):] {
	// 	return false
	// }
	// return true
}
