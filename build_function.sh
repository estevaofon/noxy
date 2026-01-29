#!/bin/bash
set -e

echo "Building Noxy Lambda Function..."

# 1. Prepare Function Directory
echo "Preparing artifacts..."
rm -rf function_dist
mkdir -p function_dist

# 2. Copy Function Code
# Just the function logic. The runtime is in the layer.
cp noxy_examples/aws_lambda/function.nx function_dist/function.nx

# 3. Zip it up
echo "Creating function.zip..."
rm -f function.zip
cd function_dist
zip -r ../function.zip .
cd ..

echo "--------------------------------------------------"
echo "Function ready: function.zip"
echo "Deploy this to Lambda with 'Custom Runtime' and the Noxy Layer attached."
echo "--------------------------------------------------"
