// Command tavilydriftscrub removes volatile identifiers, timing, and
// third-party result content before a Tavily response is persisted.
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
	decoder := json.NewDecoder(io.LimitReader(input, 2<<20))
	decoder.UseNumber()
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("decode Tavily response: %w", err)
	}
	results, ok := response["results"].([]any)
	if !ok {
		return fmt.Errorf("Tavily response has no results array")
	}
	response["request_id"] = "<redacted>"
	response["response_time"] = json.Number("0")
	redactString(response, "answer")
	if images, ok := response["images"].([]any); ok {
		for i, image := range images {
			if _, ok := image.(string); ok {
				images[i] = "<redacted>"
			}
		}
	}
	for _, item := range results {
		result, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("Tavily result must be an object")
		}
		for _, field := range []string{"title", "url", "content", "raw_content", "favicon", "published_date"} {
			redactString(result, field)
		}
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response)
}

func redactString(object map[string]any, field string) {
	if _, ok := object[field].(string); ok {
		object[field] = "<redacted>"
	}
}
