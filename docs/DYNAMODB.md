# DynamoDB Plugin for Noxy

To install follow the instructions presented in the noxy_dynamodb repository:
[https://github.com/estevaofon/noxy_dynamodb](https://github.com/estevaofon/noxy_dynamodb)

## Usage Example

```noxy
use github_com.estevaofon.noxy_dynamodb as dynamodb

func main() -> void
    // 1. Connect (Uses default AWS credentials)
    let client: dynamodb.Client = dynamodb.connect({"region": "us-east-1"})

    // 2. Put Item
    let item: map[string, any] = {
        "id": "user_123",
        "name": "Estevao",
        "email": "estevao@example.com"
    }
    
    if dynamodb.put_item(client, "Users", item) then
        print("Item saved successfully!")
    end

    // 3. Get Item
    let key: map[string, any] = {"id": "user_123"}
    let result: map[string, any] = dynamodb.get_item(client, "Users", key)
    
    if result != null then
        print(f"Found user: {result['name']}")
    end
end

main()
```