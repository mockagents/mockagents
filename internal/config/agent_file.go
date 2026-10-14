package config

import (
	"errors"
	"io/fs"
	"os"
)

// AgentFileReplaceable reports whether an agent write may persist agent name
// (owned by tenantID) to path, a filename derived from the agent's name. That
// is safe when nothing is there yet, or when the file already holds exactly
// that agent. Anything else — another agent (an example file named after one
// agent but defining another), a different kind of document, or a file that no
// longer parses — belongs to something else and must not be overwritten: the
// definition it held would silently disappear on the next restart.
func AgentFileReplaceable(path, name, tenantID string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	docs, err := LoadDocumentFile(path)
	if err != nil || docs.Count() != 1 || len(docs.Agents) != 1 {
		return false, nil
	}
	md := docs.Agents[0].Definition.Metadata
	return md.Name == name && md.TenantID == tenantID, nil
}
