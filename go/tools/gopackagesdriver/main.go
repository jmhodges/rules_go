// gopackagesdriver provides the interface between go/packages (and consumers of
// go/packages like gopls) and bazel's rules_go code to provide cross-editor
// IDE-style support. It, like other bazel tooling, expects to be run in the
// top-level directory of a bazel project.
//
// The path to its binary is meant to be set to the
// $GOPACKAGESDRIVER environment variable or installed as gopackagesdriver in a
// user's path so that go/packages can use it.
//
package main

import (
	"log"
	"os"
	"os/exec"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("gopackagesdriver: expected to be called with a go/packages pattern to query")
	}
	// We call out to gopackagesdriver-nonshim so that it can poke around in
	// GOROOT, to keep gopackagesdriver and the stdlib package list up to date
	// with the actual Go and rules_go versions the user actually has, and to
	// allow for us to generate the Go protobuf code without having to check it
	// in, while allowing gopackagesdriver itself to be installed with the
	// normal go build tooling.
	//
	// You might notice that gopackagesdriver-nonshim also has to call out ot
	// bazel and ask if we couldn't do the work in gopackagesdriver-nonshim in go
	// binaries called from the bazel aspects that gopackagesdriver-nonshim uses.
	// We, unfortunately, cannot because part of the problem for
	// gopackagesdriver-nonshim is to solve is turning Go import paths into bazel
	// labels which can include stdlib import paths that rules_go does not
	// actaully have go_library targets. Doing that work requires the go_context
	// set up for gopackagesdriver-nonshim's querytool.
	//
	// Along the way in there, it's nice to just shortcut the stdlib
	// packages.Package generation by doing it in gopackagesdriver-nonshim.
	cmd := exec.Command("bazel", "run", "@io_bazel_rules_go//go/tools/gopackagesdriver-nonshim:querytool", "--")
	cmd.Args = append(cmd.Args, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("error running gopackagesdriver bazel tooling: %v", err)
	}

}
