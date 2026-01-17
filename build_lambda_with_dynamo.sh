#!/bin/bash
set -e

echo "Building Noxy + DynamoDB Plugin for AWS Lambda (Linux/AMD64)..."

# 1. Build Binaries
# Ensure we are building for Linux
export GOOS=linux
export GOARCH=amd64

echo "Compiling noxy..."
go build -o noxy ./cmd/noxy

echo "Compiling noxy-plugin-dynamodb..."
go -C cmd/noxy-plugin-dynamodb build -o ../../noxy-plugin-dynamodb

# 2. Prepare Distribution Directory
echo "Preparing artifacts..."
rm -rf lambda_dist_dynamo
mkdir -p lambda_dist_dynamo

# Copy binaries
cp noxy lambda_dist_dynamo/
cp noxy-plugin-dynamodb lambda_dist_dynamo/

# Copy Lambda Runtime Environment (Bootstrap + Runtime Loop)
# We assume these exist from previous project structure
cp noxy_examples/aws_lambda/bootstrap lambda_dist_dynamo/
cp noxy_examples/aws_lambda/runtime.nx lambda_dist_dynamo/
cp noxy_examples/aws_lambda/lambda_types.nx lambda_dist_dynamo/
cp noxy_examples/aws_lambda/exec_runtime.nx lambda_dist_dynamo/

# Copy the actual Function Logic
# Modify this line to point to your specific Lambda handler script using DynamoDB
cp noxy_examples/aws_lambda/function.nx lambda_dist_dynamo/function.nx

# Copy Library Wrappers
cp internal/stdlib/dynamodb.nx lambda_dist_dynamo/

# 3. Set Permissions (Critical for AWS Lambda)
echo "Setting permissions..."
chmod +x lambda_dist_dynamo/bootstrap
chmod +x lambda_dist_dynamo/noxy
chmod +x lambda_dist_dynamo/noxy-plugin-dynamodb

# 4. Zip it up
echo "Creating deploy_dynamo.zip..."
rm -f deploy_dynamo.zip
cd lambda_dist_dynamo
zip -r ../deploy_dynamo.zip .
cd ..

echo "--------------------------------------------------"
echo "Success! 'deploy_dynamo.zip' is ready."
echo "Ensure your function.nx imports 'dynamodb' and includes the logic."
echo "--------------------------------------------------"
