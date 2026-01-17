package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"
)

// RPC Types (Must match internal/plugin/plugin.go)
type PluginRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

type PluginResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// Global State
var (
	Clients     = make(map[string]*dynamodb.Client)
	ClientsLock sync.Mutex
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	// Output must be line buffered JSON
	encoder := json.NewEncoder(os.Stdout)

	// Optional: Debug log
	// debugLog, _ := os.Create("plugin_debug.log")
	// defer debugLog.Close()

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req PluginRequest
		if err := json.Unmarshal(line, &req); err != nil {
			sendError(encoder, fmt.Sprintf("Parse error: %v", err))
			continue
		}

		// Handle Request
		res, err := handleRequest(req)
		response := PluginResponse{Result: res}
		if err != nil {
			response.Error = err.Error()
		}

		if err := encoder.Encode(response); err != nil {
			// Panic or log?
			fmt.Fprintf(os.Stderr, "Failed to encode response: %v\n", err)
		}
	}
}

func sendError(enc *json.Encoder, msg string) {
	enc.Encode(PluginResponse{Error: msg})
}

func handleRequest(req PluginRequest) (interface{}, error) {
	switch req.Method {
	case "connect":
		return handleConnect(req.Params)
	case "put_item":
		return handlePutItem(req.Params)
	case "get_item":
		return handleGetItem(req.Params)
	case "update_item":
		return handleUpdateItem(req.Params)
	case "delete_item":
		return handleDeleteItem(req.Params)
	case "scan":
		return handleScan(req.Params)
	case "query":
		return handleQuery(req.Params)
	default:
		return nil, fmt.Errorf("unknown method: %s", req.Method)
	}
}

func handleConnect(params []interface{}) (interface{}, error) {
	// Params: [options_map]
	if len(params) < 1 {
		return nil, fmt.Errorf("expected options map")
	}

	options, ok := params[0].(map[string]interface{})
	if !ok {
		// Tolerant if empty?
		options = make(map[string]interface{})
	}

	region := "us-east-1"
	if r, ok := options["region"].(string); ok {
		region = r
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load aws config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)
	clientId := uuid.New().String()

	ClientsLock.Lock()
	Clients[clientId] = client
	ClientsLock.Unlock()

	return clientId, nil
}

func handlePutItem(params []interface{}) (interface{}, error) {
	// Params: [clientId, tableName, itemMap]
	if len(params) < 3 {
		return nil, fmt.Errorf("expected client_id, table, item")
	}

	clientId, _ := params[0].(string)
	tableName, _ := params[1].(string)
	itemMap, ok := params[2].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("item must be a map")
	}

	client := getClient(clientId)
	if client == nil {
		return nil, fmt.Errorf("client not found: %s", clientId)
	}

	av, err := attributevalue.MarshalMap(itemMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal item: %v", err)
	}

	_, err = client.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item:      av,
	})

	if err != nil {
		return nil, err
	}

	return true, nil
}

func handleGetItem(params []interface{}) (interface{}, error) {
	// Params: [clientId, tableName, keyMap]
	if len(params) < 3 {
		return nil, fmt.Errorf("expected client_id, table, key")
	}

	clientId, _ := params[0].(string)
	tableName, _ := params[1].(string)
	keyMap, ok := params[2].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("key must be a map")
	}

	client := getClient(clientId)
	if client == nil {
		return nil, fmt.Errorf("client not found: %s", clientId)
	}

	avKey, err := attributevalue.MarshalMap(keyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %v", err)
	}

	out, err := client.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key:       avKey,
	})

	if err != nil {
		return nil, err
	}

	if out.Item == nil {
		return nil, nil // Not found = null
	}

	var resMap map[string]interface{}
	if err := attributevalue.UnmarshalMap(out.Item, &resMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %v", err)
	}

	return resMap, nil
}

func handleDeleteItem(params []interface{}) (interface{}, error) {
	// Params: [clientId, tableName, keyMap]
	if len(params) < 3 {
		return nil, fmt.Errorf("expected client_id, table, key")
	}

	clientId, _ := params[0].(string)
	tableName, _ := params[1].(string)
	keyMap, ok := params[2].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("key must be a map")
	}

	client := getClient(clientId)
	if client == nil {
		return nil, fmt.Errorf("client not found: %s", clientId)
	}

	avKey, err := attributevalue.MarshalMap(keyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %v", err)
	}

	_, err = client.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key:       avKey,
	})

	if err != nil {
		return nil, err
	}

	return true, nil
}

func handleUpdateItem(params []interface{}) (interface{}, error) {
	// Params: [clientId, tableName, keyMap, updateExpression, expressionAttributeValues]
	// Simplified Update: support detailed update expression
	if len(params) < 5 {
		return nil, fmt.Errorf("expected client_id, table, key, updateExpr, exprAttrValues")
	}

	clientId, _ := params[0].(string)
	tableName, _ := params[1].(string)
	keyMap, ok := params[2].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("key must be a map")
	}

	updateExpr, _ := params[3].(string)
	exprAttrVals, ok := params[4].(map[string]interface{})
	if !ok {
		// Allow nil/empty?
		exprAttrVals = make(map[string]interface{})
	}

	client := getClient(clientId)
	if client == nil {
		return nil, fmt.Errorf("client not found: %s", clientId)
	}

	avKey, err := attributevalue.MarshalMap(keyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %v", err)
	}

	avVals, err := attributevalue.MarshalMap(exprAttrVals)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal expr values: %v", err)
	}

	// expressionAttributeNames? For now simple implementation.

	_, err = client.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
		TableName:                 aws.String(tableName),
		Key:                       avKey,
		UpdateExpression:          aws.String(updateExpr),
		ExpressionAttributeValues: avVals,
	})

	if err != nil {
		return nil, err
	}

	return true, nil
}

func handleScan(params []interface{}) (interface{}, error) {
	// Params: [clientId, tableName, limit?]
	if len(params) < 2 {
		return nil, fmt.Errorf("expected client_id, table")
	}

	clientId, _ := params[0].(string)
	tableName, _ := params[1].(string)

	client := getClient(clientId)
	if client == nil {
		return nil, fmt.Errorf("client not found: %s", clientId)
	}

	// Basic scan, maybe add filter expressions later if requested
	in := &dynamodb.ScanInput{
		TableName: aws.String(tableName),
	}

	out, err := client.Scan(context.TODO(), in)
	if err != nil {
		return nil, err
	}

	var items []map[string]interface{}
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal items: %v", err)
	}

	return items, nil
}

func handleQuery(params []interface{}) (interface{}, error) {
	// Params: [clientId, tableName, keyConditionExpr, exprAttrValues]
	if len(params) < 4 {
		return nil, fmt.Errorf("expected client_id, table, keyCondition, exprValues")
	}

	clientId, _ := params[0].(string)
	tableName, _ := params[1].(string)
	keyCond, _ := params[2].(string)
	valMap, ok := params[3].(map[string]interface{})
	if !ok {
		valMap = make(map[string]interface{})
	}

	client := getClient(clientId)
	if client == nil {
		return nil, fmt.Errorf("client not found: %s", clientId)
	}

	avVals, err := attributevalue.MarshalMap(valMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query values: %v", err)
	}

	in := &dynamodb.QueryInput{
		TableName:                 aws.String(tableName),
		KeyConditionExpression:    aws.String(keyCond),
		ExpressionAttributeValues: avVals,
	}

	out, err := client.Query(context.TODO(), in)
	if err != nil {
		return nil, err
	}

	var items []map[string]interface{}
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal items: %v", err)
	}

	return items, nil
}

func getClient(id string) *dynamodb.Client {
	ClientsLock.Lock()
	defer ClientsLock.Unlock()
	return Clients[id]
}
