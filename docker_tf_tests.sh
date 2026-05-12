#!/bin/bash

docker rm -f cockroach_tf_tests_server || true
docker volume rm cockroach_tf_tests || true

docker volume create cockroach_tf_tests
docker run \
  --name="cockroach_tf_tests_server" \
  -d \
  --rm \
  -p 26020:26257 \
  -e COCKROACH_DATABASE=tf_tests -e COCKROACH_USER=root -e COCKROACH_PASSWORD= \
  -v "cockroach_tf_tests:/cockroach/cockroach-data" \
  cockroachdb/cockroach:latest-v24.1 \
  start-single-node --insecure
