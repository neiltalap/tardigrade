# Copyright 2017 The Cockroach Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
# implied. See the License for the specific language governing
# permissions and limitations under the License.

# NB: Despite CMake's portability, this build configuration makes no attempt to
# support non-GCC-like compilers.

# The CXX_STANDARD property was introduced in version 3.1
# 3.3 fixes https://cmake.org/cmake/help/v3.3/policy/CMP0060.html
cmake_minimum_required(VERSION 3.3 FATAL_ERROR)

project(roachlib)

add_library(roach
  batch.cc
  cache.cc
  chunked_buffer.cc
  comparator.cc
  db.cc
  encoding.cc
  engine.cc
  eventlistener.cc
  file_registry.cc
  getter.cc
  godefs.cc
  incremental_iterator.cc
  row_counter.cc
  iterator.cc
  ldb.cc
  merge.cc
  mvcc.cc
  options.cc
  snapshot.cc
  sst_dump.cc
  table_props.cc
  utils.cc
  protos/roachpb/data.pb.cc
  protos/roachpb/errors.pb.cc
  protos/roachpb/internal.pb.cc
  protos/roachpb/metadata.pb.cc
  protos/storage/engine/enginepb/mvcc.pb.cc
  protos/storage/engine/enginepb/mvcc3.pb.cc
  protos/storage/engine/enginepb/file_registry.pb.cc
  protos/storage/engine/enginepb/rocksdb.pb.cc
  protos/util/hlc/legacy_timestamp.pb.cc
  protos/util/hlc/timestamp.pb.cc
  protos/util/log/log.pb.cc
  protos/util/unresolved_addr.pb.cc
  protos/roachpb/api.pb.cc
  protos/util/tracing/recorded_span.pb.cc
  rocksdbutils/env_encryption.cc
)
target_include_directories(roach
  PUBLIC  ./include
  PRIVATE ../protobuf/src
  PRIVATE ../rocksdb/include
  PRIVATE protos
)

set_target_properties(roach PROPERTIES
  CXX_STANDARD 11
  CXX_STANDARD_REQUIRED YES
  CXX_EXTENSIONS NO
  COMPILE_OPTIONS "-Werror;-Wall;-Wno-sign-compare"
)
