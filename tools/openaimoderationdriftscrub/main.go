// Command openaimoderationdriftscrub removes volatile identifiers from an
// OpenAI moderation response before it is persisted as a drift artifact.
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
		return fmt.Errorf("decode OpenAI moderation response: %w", err)
	}
	if _, ok := response["results"]; !ok {
		return fmt.Errorf("OpenAI moderation response has no results field")
	}
	response["id"] = "<redacted>"
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response)
}
