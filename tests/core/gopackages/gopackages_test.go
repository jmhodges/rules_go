package gopackages_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"go/importer"
	"go/token"
	"io"
	"io/ioutil"
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
		// FIXME add go_binary with `library` tag.
		Nogo: "@//:gopackagesdriver_nogo",
		Main: `
-- BUILD.bazel --
load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test", "nogo")

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
	srcs = [
        "hascgo.go",
        "nocgo.go",
    ],
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

go_binary(
    name = "simplebin",
    srcs = ["simplebin.go"],
    deps = [":hello"],
    visibility = ["//visibility:public"],
)

go_library(
    name = "embedlib",
    srcs = ["embedme.go"],
    importpath = "anotherfakepath/embedlib",
    visibility = ["//visibility:public"],
)

go_binary(
    name = "embedbin",
    embed = [":embedlib"],
    visibility = ["//visibility:public"],
)

go_test(
    name = "hello_use_test",
    srcs = ["hello_use_test.go"],
    deps = [":hello_use"],
)

go_test(
    name = "no_deps_test",
    srcs = ["simple_test.go"]
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
-- nocgo.go --
package hascgo

func K() int { return 1 }

-- hello_use.go --
package hello_use

import "fakeimportpath/hello"

func K() string {
	return hello.A()
}
-- hello_use_test.go --
package hello_use

import "testing"

func TestK(t *testing.T) {
	if K() != "hello is 12" {
		t.Errorf("welp")
	}
}
-- simplebin.go --
package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
-- embedme.go --
package main

import "fmt"

func main() {
	fmt.Println("Hello, embedded library World!")
}
-- simple_test.go --
package main

import "testing"

func TestGood(t *testing.T) {
	t.Skip()
}
`,
	})
}

// FIXME rename file to gopackagesdriver_test.go
func TestPatterns(t *testing.T) {
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
		inputPatterns []string
		mode          packages.LoadMode
		outputPkgs    []*packages.Package
	}{
		{
			[]string{"//:hello"},
			packages.NeedName,
			[]*packages.Package{
				{
					ID:      "//:hello",
					Name:    "hello",
					PkgPath: "fakeimportpath/hello",
				},
			},
		},
		{
			[]string{"//:hello"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				{
					ID:      "//:hello",
					Name:    "hello",
					PkgPath: "fakeimportpath/hello",
					GoFiles: []string{abs("hello.go")},
				},
			},
		},
		{
			[]string{"//:hello"},
			packages.NeedName | packages.NeedImports,
			[]*packages.Package{
				{
					ID:      "//:hello",
					Name:    "hello",
					PkgPath: "fakeimportpath/hello",
					Imports: map[string]*packages.Package{
						"fmt": fmtPkg,
					},
				},
			},
		},
		{
			[]string{"//:hello_use"},
			packages.NeedName,
			[]*packages.Package{
				{
					ID:      "//:hello_use",
					Name:    "hello_use",
					PkgPath: "fakeimportpath/hello_use",
					Imports: nil,
				},
			},
		},
		{
			[]string{"//:hello_use"},
			packages.NeedName | packages.NeedImports,
			[]*packages.Package{
				{
					ID:      "//:hello_use",
					Name:    "hello_use",
					PkgPath: "fakeimportpath/hello_use",
					Imports: map[string]*packages.Package{
						"fakeimportpath/hello": &packages.Package{
							ID:      "//:hello",
							Imports: make(map[string]*packages.Package),
						},
					},
				},
			},
		},
		{
			[]string{"//:hello_use"},
			packages.NeedName | packages.NeedDeps | packages.NeedImports,
			[]*packages.Package{
				{
					ID:      "//:hello_use",
					Name:    "hello_use",
					PkgPath: "fakeimportpath/hello_use",
					Imports: map[string]*packages.Package{
						"fakeimportpath/hello": &packages.Package{
							ID:      "//:hello",
							Name:    "hello",
							PkgPath: "fakeimportpath/hello",
							Imports: map[string]*packages.Package{
								"fmt": &packages.Package{
									ID:      "@go_sdk//stdlibstub/fmt",
									Name:    "fmt",
									PkgPath: "fmt",
									Imports: make(map[string]*packages.Package),
								},
							},
						},
					},
				},
			},
		},
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
					Imports: map[string]*packages.Package{
						"fmt": &packages.Package{
							ID:      "@go_sdk//stdlibstub/fmt",
							Imports: make(map[string]*packages.Package),
						},
					},
				},
				&packages.Package{
					ID:      "//:hello",
					Name:    "hello",
					PkgPath: "fakeimportpath/hello",
					Imports: map[string]*packages.Package{
						"fmt": &packages.Package{
							ID:      "@go_sdk//stdlibstub/fmt",
							Imports: make(map[string]*packages.Package),
						},
					},
				},
			},
		},
		{
			[]string{"//:simplebin"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				&packages.Package{
					ID:      "//:simplebin",
					Name:    "main",
					PkgPath: "simplebin",
					GoFiles: []string{abs("simplebin.go")},
				},
			},
		},
		{
			[]string{"file=embedme.go"},
			packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps,
			[]*packages.Package{
				&packages.Package{
					ID: "//:embedlib",
					// FIXME this should be "main", but we don't currently pull that data.
					Name:    "embedlib",
					PkgPath: "anotherfakepath/embedlib",
					GoFiles: []string{abs("embedme.go")},
					Imports: map[string]*packages.Package{
						"fmt": &packages.Package{
							ID:      "@go_sdk//stdlibstub/fmt",
							Name:    "fmt",
							PkgPath: "fmt",
							GoFiles: []string{
								abs("external/go_sdk/src/fmt/doc.go"),
								abs("external/go_sdk/src/fmt/errors.go"),
								abs("external/go_sdk/src/fmt/format.go"),
								abs("external/go_sdk/src/fmt/print.go"),
								abs("external/go_sdk/src/fmt/scan.go"),
							},
							Imports: make(map[string]*packages.Package),
						},
					},
				},
			},
		},
		{
			[]string{"//:hascgo"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				{
					ID:      "//:hascgo",
					Name:    "hascgo",
					PkgPath: "fakeimportpath/hascgo",
					GoFiles: []string{abs("hascgo.go"), abs("nocgo.go")},
				},
			},
		},
		{
			[]string{"//:hascgo"},
			packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles,
			[]*packages.Package{
				{
					ID:      "//:hascgo",
					Name:    "hascgo",
					PkgPath: "fakeimportpath/hascgo",
					GoFiles: []string{abs("hascgo.go"), abs("nocgo.go")},
					CompiledGoFiles: []string{
						abs("hascgo.go"),
						abs("nocgo.go"),
						abs("_cgo_imports.go"),
					},
				},
			},
		},
		{
			[]string{"//:hello"},
			packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles,
			[]*packages.Package{
				{
					ID:              "//:hello",
					Name:            "hello",
					PkgPath:         "fakeimportpath/hello",
					GoFiles:         []string{abs("hello.go")},
					CompiledGoFiles: []string{abs("hello.go")},
				},
			},
		},
		{
			[]string{"file=hello_use_test.go"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				{
					ID:      "//:hello_use_test",
					Name:    "hello_use [test]",
					PkgPath: "fakeimportpath/hello_use",
					GoFiles: []string{abs("hello_use_test.go")},
				},
			},
		},
		{
			[]string{"//:hello_use_test"},
			packages.NeedName | packages.NeedFiles,
			[]*packages.Package{
				{
					ID:      "//:hello_use_test",
					Name:    "hello_use [test]",
					PkgPath: "fakeimportpath/hello_use",
					GoFiles: []string{abs("hello_use_test.go")},
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
					Logf:    t.Logf,
				}
				pkgs, err := packages.Load(cfg, tc.inputPatterns...)
				if err != nil {
					t.Fatalf("unable to packages.Load(mode: %d, patterns: %s): %s", tc.mode, tc.inputPatterns, err)
				}
				if len(pkgs) != len(tc.outputPkgs) {
					t.Errorf("too many packages returned: want %d, got %d", len(tc.outputPkgs), len(pkgs))
				}
				if !cmp.Equal(tc.outputPkgs, pkgs, pkgCmpOpt) {
					t.Errorf("packages from patterns %s, mode %d didn't match, diff: %s", tc.inputPatterns, tc.mode, cmp.Diff(tc.outputPkgs, pkgs, pkgCmpOpt))
				}
			})
	}
}

type contentsOrError struct {
	Contents string
	Err      error
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
		"TurnFilePathsIntoFileContents",
		func(xs []string) []contentsOrError {
			out := make([]contentsOrError, len(xs))
			for i, x := range xs {
				// _cgo_imports.go is created by compilepkg's cgo code and won't
				// exist on disk before the compile is made. That means we can't
				// do our usual transformation on the expected side. The
				// _cgo_imports.go generated will be platform (and, I believe Go
				// version) specific, so let's skip trying to maintain that. If
				// that turns to be easier than I think, we can rip this back
				// out.
				if filepath.Base(x) == "_cgo_imports.go" {
					out[i] = contentsOrError{Contents: "fakecgosha", Err: nil}
					continue
				}
				contents, err := ioutil.ReadFile(resolveLink(x))
				out[i] = contentsOrError{Contents: string(contents), Err: err}
			}
			return out
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
	if err := compareFiles(expectedGoFiles, pkg.GoFiles); err != nil {
		t.Errorf("GoFiles: expected contenst of  %s didn't match those of %s: %s", expectedGoFiles, pkg.GoFiles, err)
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
		pkgs, err = packages.Load(cfg, fmt.Sprintf("file=%s", abs("goodbye_other.go")))
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
		if err := compareFiles(expectedGoFiles, pkg.GoFiles); err != nil {
			t.Errorf("absolute path, GoFiles, error: %s", err)
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
	if err := compareFiles(expectedCompiledGoFiles, pkg.CompiledGoFiles); err != nil {
		t.Errorf("CompiledGoFiles: contents of expected files %s didn't match those of %s: %s", expectedCompiledGoFiles, pkg.CompiledGoFiles, err)
	}
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
		Mode:    packages.NeedExportsFile | packages.NeedName | packages.NeedImports | packages.NeedDeps,
		Context: ctx,
	}
	loadPkgs, err := packages.Load(cfg, "//:hello")
	if err != nil {
		t.Fatalf("unable to packages.Load: %s", err)
	}

	if len(loadPkgs) < 1 {
		t.Fatalf("no packages returned")
	}
	if len(loadPkgs) != 1 {
		t.Errorf("too many packages returned: want 1, got %d", len(loadPkgs))
	}
	pkg := loadPkgs[0]
	expectedID := "//:hello"
	if pkg.ID != expectedID {
		t.Errorf("ID: want %#v, got %#v", expectedID, pkg.ID)
	}
	if filepath.Base(pkg.ExportFile) != "hello.a" {
		t.Errorf("ExportFile: expected export file to end in 'hello.a', but was %s", pkg.ExportFile)
	}
	libs := make(map[string]string)
	var visit func(ps map[string]string, p *packages.Package)
	visit = func(ps map[string]string, p *packages.Package) {
		libs[p.PkgPath] = p.ExportFile
		for _, ipkg := range p.Imports {
			visit(ps, ipkg)
		}
	}
	visit(libs, pkg)
	fset := token.NewFileSet()
	lookup := func(path string) (io.ReadCloser, error) {
		exportfile, found := libs[path]
		if !found {
			return nil, fmt.Errorf("unknown import path %s given to our test lookup func (we have %s)", path, libs)
		}
		return os.Open(exportfile)
	}
	impr := importer.ForCompiler(fset, "gc", lookup)
	imprPkg, err := impr.Import("fakeimportpath/hello")
	if err != nil {
		t.Fatalf("error returned trying to import hello: %s", err)
	}
	if imprPkg.Name() != "hello" {
		t.Errorf("Name: want \"hello\", got %#v", imprPkg.Name())
	}
	if imprPkg.Path() != "fakeimportpath/hello" {
		t.Errorf("Name: want \"fakeimportpath/hello\", got %#v", imprPkg.Path())
	}
	if len(pkg.Imports) != len(imprPkg.Imports()) {
		t.Errorf("Imports: want %d imports, got %d imports", len(pkg.Imports), len(imprPkg.Imports()))
	} else {
		type commonPkg struct {
			Name    string
			PkgPath string
		}
		var expected []commonPkg
		var actual []commonPkg
		for _, ipkg := range pkg.Imports {
			expected = append(expected, commonPkg{Name: ipkg.Name, PkgPath: ipkg.PkgPath})
		}
		for _, imprIPkg := range imprPkg.Imports() {
			actual = append(actual, commonPkg{Name: imprIPkg.Name(), PkgPath: imprIPkg.Path()})
		}
		if !cmp.Equal(expected, actual) {
			t.Errorf("Imports, diff: %s", cmp.Diff(expected, actual))
		}
	}
	// FIXME test cgo version.
}

func TestStdlib(t *testing.T) {
	// FIXME stdlib command packages should have "main" for their Name
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

func compareFiles(expected, actual []string) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("number of files expected was %d, but got %d", len(expected), len(actual))
	}
	for i, exp := range expected {
		if err := compareFile(exp, actual[i]); err != nil {
			return err
		}
	}
	return nil
}

func compareFile(expected, actual string) error {
	// Between bazel and bazel_testing's symlinking and cd'ing, comparing the
	// file paths to source code we can easily construct in these tests and the
	// file paths that gopackagesdriver-nonshim has the context to construct is
	// a no go. Things like symlinks of directories a few levels above the base
	// file, etc., make comparing them difficult, at best.  So, we've got to
	// pick up the actual files and compare the contents.
	exp, err := shasum(abs(expected))
	if err != nil {
		return fmt.Errorf("error hashing contents of expected %#v path: %w", expected, err)
	}
	act, err := shasum(resolveLink(actual))
	if err != nil {
		return fmt.Errorf("error hashing contents of actual output %#v path: %w", actual, err)
	}
	if exp != act {
		return fmt.Errorf("sha256 of expected file %#v and actual file %#v contents didn't match", expected, actual)
	}
	return nil
}

func shasum(fp string) ([sha256.Size]byte, error) {
	b, err := ioutil.ReadFile(fp)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("unable to read file %#v: %s", fp, err)
	}
	return sha256.Sum256(b), nil
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
