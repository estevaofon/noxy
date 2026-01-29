#!/bin/bash
set -e

echo "Building Noxy Lambda Layer..."

# 1. Build Binaries
# Ensure we are building for Linux
export GOOS=linux
export GOARCH=amd64

echo "Compiling noxy..."
go build -o noxy ./cmd/noxy

echo "Compiling noxy-plugin-dynamodb..."
go -C noxy_libs/github_com/estevaofon/noxy_dynamodb build -o ../../../../noxy-plugin-dynamodb

# 2. Prepare Layer Directory
echo "Preparing layer structure..."
rm -rf layer_dist
mkdir -p layer_dist/bin
mkdir -p layer_dist/noxy_libs/dynamodb

# Copy binaries to /bin (will be in PATH)
cp noxy layer_dist/bin/
cp noxy_examples/aws_lambda/bootstrap layer_dist/

# Copy Runtime Files to root of layer? Or /lib?
# Standard practice: put libraries in /opt/noxy_libs or similar.
# We will put runtime files in /opt/runtime
mkdir -p layer_dist/runtime
cp noxy_examples/aws_lambda/runtime.nx layer_dist/runtime/
cp noxy_examples/aws_lambda/lambda_types.nx layer_dist/runtime/
cp noxy_examples/aws_lambda/exec_runtime.nx layer_dist/runtime/

# Copy Libraries
cp noxy-plugin-dynamodb layer_dist/bin/
cp noxy_libs/github_com/estevaofon/noxy_dynamodb/dynamodb.nx layer_dist/noxy_libs/dynamodb/

# Set Permissions (Critical for AWS Lambda)
chmod +x layer_dist/bootstrap
chmod +x layer_dist/bin/noxy
chmod +x layer_dist/bin/noxy-plugin-dynamodb

# 3. Zip it up
echo "Creating noxy_layer.zip..."
rm -f noxy_layer.zip
cd layer_dist
zip -r ../noxy_layer.zip .
cd ..

echo "--------------------------------------------------"
echo "Layer ready: noxy_layer.zip"
echo "Structure:"
echo "  /bootstrap"
echo "  /bin/noxy"
echo "  /runtime/*.nx"
echo "  /noxy_libs/..."
echo "--------------------------------------------------"
