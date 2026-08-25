// Command coheredriftscrub removes volatile and billing fields from a Cohere
// v2 rerank response before it is persisted as a drift artifact.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := scrub(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func scrub(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	decoder.UseNumber()
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("decode Cohere response: %w", err)
	}
	if _, ok := response["results"]; !ok {
		return fmt.Errorf("Cohere response has no results field")
	}
	response["id"] = "<redacted>"
	if meta, ok := response["meta"].(map[string]any); ok {
		delete(meta, "billed_units")
		delete(meta, "tokens")
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response)
}
