Write-Host "Building DynamoDB Plugin..."
go mod tidy
go build -o noxy-plugin-dynamodb.exe .
Write-Host "Done. Created noxy-plugin-dynamodb.exe"
