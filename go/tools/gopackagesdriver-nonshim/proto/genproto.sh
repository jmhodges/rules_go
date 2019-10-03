#!/bin/bash

# FIXME now that we keep nonshim in rules_go, this can be replaced with normal
# go proto library stuff.

set -euo pipefail
cd "$(dirname "$0")"
M=Msrc/main/protobuf/invocation_policy.proto=github.com/bazelbuild/rules_go/go/tools/gopackagesdriver-nonshim/proto/invocation_policy
M=$M,Msrc/main/protobuf/command_line.proto=github.com/bazelbuild/rules_go/go/tools/gopackagesdriver-nonshim/proto/command_line
M=$M,Msrc/main/protobuf/option_filters.proto=github.com/bazelbuild/rules_go/go/tools/gopackagesdriver-nonshim/proto/option_filters
protos=(
  build_event_stream.proto
  src/main/protobuf/invocation_policy.proto
  src/main/protobuf/command_line.proto
  src/main/protobuf/option_filters.proto
)

for proto in "${protos[@]}"; do
  protoc --go_out="${M}:." "$proto"
  mv "$(dirname "$proto")/$(basename "$proto" .proto).pb.go" "$(basename "$proto" .proto)"
done
