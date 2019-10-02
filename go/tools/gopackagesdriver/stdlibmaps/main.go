package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"text/template"
)

var (
	pkgList = flag.String("pkglist", "", "the file path to packages_list (required)")
	goroot  = flag.String("goroot", "", "the file path of the GOROOT directory (required)")
	pkgName = flag.String("pkgname", "stdlibmaps", "the name to use in the 'package' statement in the generated file")
)

type tmplData struct {
	GenPkgName string
	Pkgs       []pkg
	VendorPkgs []pkg
}

type pkg struct {
	StdPkgImport     string
	StdPkgBazelLabel string
}

const stdlibLabelFmt = "@go_sdk//stdlib/:%s"

func main() {
	// FIXME using this style means we either have to check the out of this in
	// to the repo to allow `go get` to work, or it requires bazel. But that
	// latter idea might lead to people using the
	// @io_bazel_rules_go//go/tools/gopackagesdriver in their own workspace
	// which would be difficult to configure across multiple projects.

	// FIXME actually the smart thing to do would be to make gopackagesdriver
	// go-gettable by making it a very thing wrapper over a `bazel run` call of
	// binary in @io_bazel_rules_go, yeah? We'd do this so that folks always get stdlib package lists that match their
	flag.Parse()
	if *pkgList == "" {
		log.Fatalf("genfakestdlib: required `-pkgList` argument not provided")
	}
	if *goroot == "" {
		log.Fatalf("genfakestdlib: required `-goroot` argument not provided")
	}
	b, err := ioutil.ReadFile(*pkgList)
	if err != nil {
		log.Fatalf("genfakestdlib: error opening %#v: %s", *pkgList, err)
	}
	lines := bytes.Split(b, []byte{'\n'})
	pkgs := make([]pkg, 0, len(lines))
	var vendorPkgs []pkg
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		p := pkg{
			StdPkgImport:     string(line),
			StdPkgBazelLabel: fmt.Sprintf(stdlibLabelFmt, line),
		}
		if !bytes.HasPrefix(line, []byte("vendor/")) {
			pkgs = append(pkgs, p)
		} else {
			vendorPkgs = append(vendorPkgs, p)
		}
	}
	sort.Slice(
		pkgs,
		func(i, j int) bool {
			return pkgs[i].StdPkgImport < pkgs[j].StdPkgImport
		},
	)
	buf := &bytes.Buffer{}
	err = mapTmpl.Execute(buf, tmplData{
		GenPkgName: *pkgName,
		Pkgs:       pkgs,
		VendorPkgs: vendorPkgs,
	})
	if err != nil {
		log.Fatalf("genfakestdlib: unable to execute the templated file to generate the Go code: %s", err)
	}
	code, err := format.Source(buf.Bytes())
	if err != nil {
		log.Fatalf("genfakestdlib: unable to format generated Go code: %s", err)
	}
	os.Stdout.Write(code)
}

var mapTmpl = template.Must(template.New("maps").Parse(`package {{.GenPkgName}}

// StdlibImportPathToBazelLabel maps the Go standard import paths of libraries
// in the Go stdlib to their equivalent, fake bazel label. If you need to also
// look up labels of the libraries that the stdlib vendors, use
// StdlibVendorImportPathToBazelLabel.
var StdlibImportPathToBazelLabel = map[string]string{
	{{ range $i, $pkg := .Pkgs -}}
	{{ $pkg.StdPkgImport | printf "%#v" }}: {{ $pkg.StdPkgBazelLabel | printf "%#v" }},
	{{ end }}
}

// StdlibBazelLabelToImportPath maps to fake bazel labels for libraries in the
// Go standard library to their equivalent Go standard import path. It includes
// the private libraries vendored by the stdlib because the label'ing fixes the
// namespaceing so that non-stdlib targets will not refer to it.
var StdlibBazelLabelToImportPath = map[string]string{
	{{ range $i, $pkg := .Pkgs -}}
	{{ $pkg.StdPkgBazelLabel | printf "%#v" }}: {{ $pkg.StdPkgImport | printf "%#v" }},
	{{ end }}
	{{ range $i, $pkg := .VendorPkgs -}}
	{{ $pkg.StdPkgBazelLabel | printf "%#v" }}: {{ $pkg.StdPkgImport | printf "%#v" }},
	{{ end }}
}

// StdlibVendorImportPathToBazelLabel maps the Go standard import paths of the
// vendored libraries in the Go stdlib to their equivalent, fake bazel label. The
// import paths are prefixed with "vendor/".
var StdlibVendorImportPathToBazelLabel = map[string]string{
	{{ range $i, $pkg := .VendorPkgs -}}
	{{ $pkg.StdPkgImport | printf "%#v" }}: {{ $pkg.StdPkgBazelLabel | printf "%#v" }},
	{{ end }}
}

// StdlibImportPaths is a sorted list of the import path of every library in the
// Go standard library except the libraries the stdlib vendored.
var StdlibImportPaths = []string{
	{{ range $i, $pkg := .Pkgs -}}
	{{ $pkg.StdPkgImport | printf "%#v" }},
	{{ end }}
}
`))
