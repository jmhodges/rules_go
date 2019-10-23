package gopackages_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	// FIXME remove
	"github.com/bazelbuild/rules_go/go/tools/bazel_testing"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/go/packages"
)

// This is to get the platform-specific path to gopackagesdriver in the
// encompassing rules_go's bazel-bin. See the gopackages_test target in
// BUILD.bazel.
var goPkgDriverPath = flag.String("goPkgDriverPath", "", "path to the gopackagesdriver binary")
var pwd string

// FIXME should this test directory be somewhere else?
func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("unable to Getwd: %s", err)
	}
	pwd = wd
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

go_library(
    name = "hello_use",
    srcs = ["hello_use.go"],
    deps = [":hello"],
    importpath = "fakeimportpath/hello_use",
    visibility = ["//visibility:public"],
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
-- hello_use.go --
package hello_use

import "hello"

func K() string {
	hello.A()
}
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
	os.Setenv("GOPACKAGESDRIVER", driverPath) // FIXME Use Env and os.Environ

	fmtPkg := &packages.Package{
		ID:      "@go_sdk//stdlibstub/fmt",
		Imports: make(map[string]*packages.Package),
	}

	testcases := []struct {
		inputPatterns string
		mode          packages.LoadMode
		outputPkg     *packages.Package
	}{
		{
			"//:hello",
			packages.NeedName,
			&packages.Package{
				ID:      "//:hello",
				Name:    "hello",
				PkgPath: "fakeimportpath/hello",
			},
		},
		{
			"//:hello",
			packages.NeedName | packages.NeedFiles,
			&packages.Package{
				ID:      "//:hello",
				Name:    "hello",
				PkgPath: "fakeimportpath/hello",
				GoFiles: []string{abs("hello.go")},
			},
		},

		{
			"//:hello",
			packages.NeedName | packages.NeedImports,
			&packages.Package{
				ID:      "//:hello",
				Name:    "hello",
				PkgPath: "fakeimportpath/hello",
				Imports: map[string]*packages.Package{
					"fmt": fmtPkg,
				},
			},
		},
	}

	for tcInd, tc := range testcases {
		t.Run(fmt.Sprintf("test-%d", tcInd),
			func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				cfg := &packages.Config{
					Mode:    tc.mode,
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
				if !cmp.Equal(tc.outputPkg, pkgs[0], pkgCmpOpt) {
					t.Errorf("Packages didn't match, diff: %s", cmp.Diff(tc.outputPkg, pkgs[0], pkgCmpOpt))
				}
			})
	}
}

var pkgCmpOpt = cmp.FilterPath(
	func(p cmp.Path) bool {
		switch p.Last().String() {
		case ".GoFiles", ".CompiledGoFiles", ".OtherFiles":
			return true
		}
		return false
	},
	cmp.Transformer(
		"ResolveSymlinksIfTheyExist",
		func(xs []string) []string {
			for i, x := range xs {
				xs[i] = resolveLink(x)
			}
			return xs
		},
	),
)

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
	// filepath.Abs didn't work, nor does getwd before cd
	/*
		absPath := filepath.Join(pwd, "./goodbye_other.go")
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
	*/
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
	driverPath, err := getDriverPath()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GOPACKAGESDRIVER", driverPath) // FIXME Use Env and os.Environ
	imports := map[string]*packages.Package{
		"fmt": &packages.Package{
			ID:      "@go_sdk//stdlibstub/fmt",
			Imports: make(map[string]*packages.Package),
		},
	}
	testcases := []struct {
		inputPatterns []string
		mode          packages.LoadMode
		outputPkgs    []*packages.Package
	}{
		{
			[]string{"//:hello", "//:goodbye"},
			packages.NeedName,
			[]*packages.Package{
				&packages.Package{
					ID:      "//:goodbye",
					Name:    "goodbye",
					PkgPath: "fakeimportpath/goodbye",
				},
				&packages.Package{
					ID:      "//:hello",
					Name:    "hello",
					PkgPath: "fakeimportpath/hello",
				},
			},
		},
		{
			[]string{"//:hello", "//:goodbye"},
			packages.NeedName | packages.NeedImports,
			[]*packages.Package{
				&packages.Package{
					ID:      "//:goodbye",
					Name:    "goodbye",
					PkgPath: "fakeimportpath/goodbye",
					Imports: imports,
				},
				&packages.Package{
					ID:      "//:hello",
					Name:    "hello",
					PkgPath: "fakeimportpath/hello",
					Imports: imports,
				},
			},
		},
	}

	for tcInd, tc := range testcases {
		t.Run(fmt.Sprintf("test-%d", tcInd), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cfg := &packages.Config{
				Mode:    tc.mode,
				Context: ctx,
			}
			pkgs, err := packages.Load(cfg, tc.inputPatterns...)
			if err != nil {
				t.Errorf("Load: %s", err)
				return
			}
			if len(tc.outputPkgs) != len(pkgs) {
				t.Errorf("num pkgs: want %d pkgs, got %d pkgs (want %q, got %q)", len(tc.outputPkgs), len(pkgs), tc.outputPkgs, pkgs)
			} else {
				for i, exp := range tc.outputPkgs {
					if !cmp.Equal(exp, pkgs[i], pkgCmpOpt) {
						t.Errorf("package %d, diff: %s", i, cmp.Diff(exp, pkgs[i], pkgCmpOpt))
					}
				}
			}

		})
	}
}

func TestStdlib(t *testing.T) {
	// FIXME
	root, err := bazel_testing.BazelOutput("info", "execution_root")
	if err != nil {
		t.Fatalf("unable to get bazel execution_root: %s", err)
	}
	t.Logf("FIXME execution_root is %#v", string(root))

	os.Setenv("BAZEL_DROP_TEST_ENV", "1")

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
					ID:      "@go_sdk//stdlibstub/builtin",
					Name:    "builtin",
					PkgPath: "builtin",
					GoFiles: []string{abs("external/go_sdk/src/builtin/builtin.go")},
				},
			},
		},
		{
			[]string{"@go_sdk//stdlibstub/builtin"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				&packages.Package{
					ID:      "@go_sdk//stdlibstub/builtin",
					Name:    "builtin",
					PkgPath: "builtin",
					GoFiles: []string{abs("external/go_sdk/src/builtin/builtin.go")},
				},
			},
		},
		{
			[]string{"@go_sdk//stdlibstub/io/ioutil"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				&packages.Package{
					ID:      "@go_sdk//stdlibstub/io/ioutil",
					Name:    "ioutil",
					PkgPath: "io/ioutil",
					GoFiles: []string{
						abs("external/go_sdk/src/io/ioutil/ioutil.go"),
						abs("external/go_sdk/src/io/ioutil/tempfile.go"),
					},
				},
			},
		},
		{
			[]string{"@go_sdk//stdlibstub/io/ioutil"},
			packages.NeedName | packages.NeedImports,
			[]*packages.Package{
				&packages.Package{
					ID:      "@go_sdk//stdlibstub/io/ioutil",
					Name:    "ioutil",
					PkgPath: "io/ioutil",
					Imports: map[string]*packages.Package{
						"bytes": &packages.Package{
							ID:      "@go_sdk//stdlibstub/bytes",
							Imports: make(map[string]*packages.Package),
						},
						"io": &packages.Package{
							ID:      "@go_sdk//stdlibstub/io",
							Imports: make(map[string]*packages.Package),
						},
						"os": &packages.Package{
							ID:      "@go_sdk//stdlibstub/os",
							Imports: make(map[string]*packages.Package),
						},
						"path/filepath": &packages.Package{
							ID:      "@go_sdk//stdlibstub/path/filepath",
							Imports: make(map[string]*packages.Package),
						},
						"sort": &packages.Package{
							ID:      "@go_sdk//stdlibstub/sort",
							Imports: make(map[string]*packages.Package),
						},
						"strconv": &packages.Package{
							ID:      "@go_sdk//stdlibstub/strconv",
							Imports: make(map[string]*packages.Package),
						},
						"strings": &packages.Package{
							ID:      "@go_sdk//stdlibstub/strings",
							Imports: make(map[string]*packages.Package),
						},
						"sync": &packages.Package{
							ID:      "@go_sdk//stdlibstub/sync",
							Imports: make(map[string]*packages.Package),
						},
						"time": &packages.Package{
							ID:      "@go_sdk//stdlibstub/time",
							Imports: make(map[string]*packages.Package),
						},
					},
				},
			},
		},
	}

	// FIXME delete
	driverPath, err := getDriverPath()
	if err != nil {
		t.Fatal(err)
	}

	// os.Setenv("BAZEL_DROP_TEST_ENV", "1") // FIXME Use Env and os.Environ

	// FIXME using Config.Env doesn't work because gopackagesdriver isn't found
	// in the interior bazel run.
	for tcInd, tc := range testcases {
		t.Run(fmt.Sprintf("test-%d-%s", tcInd, strings.Join(tc.inputPatterns, ",")),
			func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				cfg := &packages.Config{
					Mode:    tc.mode,
					Context: ctx,
					Logf:    t.Logf,
					Env:     append(os.Environ(), fmt.Sprintf("GOPACKAGESDRIVER=%s", driverPath)),
				}
				pkgs, err := packages.Load(cfg, tc.inputPatterns...)
				if err != nil {
					t.Errorf("Load: %s", err)
					return
				}
				if len(tc.outputPkgs) != len(pkgs) {
					t.Errorf("num pkgs: want %d pkgs, got %d pkgs (want %q, got %q)", len(tc.outputPkgs), len(pkgs), tc.outputPkgs, pkgs)
				} else {
					for i, exp := range tc.outputPkgs {
						if !cmp.Equal(exp, pkgs[i], pkgCmpOpt) {
							t.Errorf("package %d, diff: %s", i, cmp.Diff(exp, pkgs[i], pkgCmpOpt))
						}
					}
				}
			})
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

func compareFile(expected, actual string) bool {
	return abs(expected) == resolveLink(actual)
}

func abs(filePath string) string {
	execRootLock.Lock()
	defer execRootLock.Unlock()
	if executionRoot == "" {
		// executionRoot = os.Getenv("TEST_TMPDIR)
		// FIXME talk to Jay about a better way of doing this.

		// FIXME follow symlink
		root, err := bazel_testing.BazelOutput("info", "execution_root")
		if err != nil {
			log.Fatalf("unable to get bazel execution_root: %s", err)
		}
		executionRoot = string(bytes.TrimSpace(root))
	}
	sym := filepath.Join(executionRoot, filePath)
	return resolveLink(sym)
}

func resolveLink(fp string) string {
	// explicit max amount of checks in case the links loop. Thank you, based
	// JPL coding standard rule 3, for making me think about this.
	for i := 0; i < 5; i++ {
		fi, err := os.Lstat(fp)
		if err != nil {
			return fp
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			newFP, err := os.Readlink(fp)
			if err != nil {
				return newFP
			}
			fp = newFP
		} else {
			return fp
		}
	}
	return fp
}

// FIXME use abs in expected values instead of the wild cmp stuff.
const srcFilePrefix = "bazel_testing/bazel_go_test/main/"

var executionRoot string
var execRootLock sync.Mutex
