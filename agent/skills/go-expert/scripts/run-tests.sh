#!/bin/bash
# run-tests.sh: Run tests for Go projects with verbose output and coverage
set -e

PACKAGE=${1:-"./..."}
echo "Running tests for $PACKAGE..."
go test -v -cover "$PACKAGE"
