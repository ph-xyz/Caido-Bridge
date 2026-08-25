package tools

import "github.com/google/jsonschema-go/jsonschema"

// schemaFor derives the schema from the same Go type consumed by the handler.
// Supplying it explicitly to mcp.AddTool makes the advertised schema and the
// validator schema a single, auditable contract.
func schemaFor[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	return schema
}
