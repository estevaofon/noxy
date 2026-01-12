#!/bin/bash
set -e

echo "Building Noxy for AWS Lambda (Linux/AMD64)..."

# 1. Build Noxy Binary
# Ensure we are building for Linux
export GOOS=linux
export GOARCH=amd64

echo "Compiling..."
go build -o noxy ./cmd/noxy

# 2. Prepare Distribution Directory
echo "Preparing artifacts..."
rm -rf lambda_dist
mkdir -p lambda_dist

# Copy artifacts
cp noxy lambda_dist/
cp noxy_examples/aws_lambda/bootstrap lambda_dist/
cp noxy_examples/aws_lambda/runtime.nx lambda_dist/
cp noxy_examples/aws_lambda/exec_runtime.nx lambda_dist/
cp noxy_examples/aws_lambda/function.nx lambda_dist/

# 3. Set Permissions (Critical for AWS Lambda)
echo "Setting permissions..."
chmod +x lambda_dist/bootstrap
chmod +x lambda_dist/noxy

# 4. Zip it up
echo "Creating deploy.zip..."
rm -f deploy.zip
cd lambda_dist
zip -r ../deploy.zip .
cd ..

echo "--------------------------------------------------"
echo "Success! 'deploy.zip' is ready for upload to AWS."
echo "--------------------------------------------------"
