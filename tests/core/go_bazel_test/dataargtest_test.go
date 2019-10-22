// Copyright 2019 The Bazel Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	bazel_testing.TestMain(m, bazel_testing.Args{})
}

// Tests that go_bazel_test keeps includes data files correctly and doesn't mess
// up on `args` that include `$(location ...)` calls.
func TestGoldenPath(t *testing.T) {
	_, err := os.Stat(filepath.Join(pwd, *binaryPath))
	if err != nil {
		t.Errorf("unable to open Go binary file: %s", err)
	}
}
