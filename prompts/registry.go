package prompts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"onboardmeplease/contracts"
)

//go:embed registry.json templates/*.txt
var packaged embed.FS

type Entry struct {
	ID              string   `json:"id"`
	Version         string   `json:"version"`
	Purpose         string   `json:"purpose"`
	TemplateRef     string   `json:"template_ref"`
	InputSchemaRef  string   `json:"input_schema_ref"`
	OutputSchemaRef string   `json:"output_schema_ref"`
	AllowedTools    []string `json:"allowed_tools"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	MaxToolCalls    int      `json:"max_tool_calls"`
	Checksum        string   `json:"checksum"`
}

type Registry struct {
	Version          string  `json:"registry_version"`
	AgentTemplateRef string  `json:"agent_template_ref"`
	AgentChecksum    string  `json:"agent_checksum"`
	Prompts          []Entry `json:"prompts"`
}

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func Validate() error {
	raw, err := packaged.ReadFile("registry.json")
	if err != nil {
		return err
	}
	var registry Registry
	if err := json.Unmarshal(raw, &registry); err != nil {
		return err
	}
	if !versionPattern.MatchString(registry.Version) || len(registry.Prompts) != 12 {
		return errors.New("prompt registry has an invalid version or missing templates")
	}
	seen := make(map[string]bool)
	allowed := map[string]bool{"exact_search": true, "lexical_search": true, "semantic_search": true, "expand_graph": true, "read_evidence": true, "find_references": true}
	for _, entry := range registry.Prompts {
		if len(entry.ID) != 3 || entry.ID[0] != 'P' || seen[entry.ID] || !versionPattern.MatchString(entry.Version) || entry.Purpose == "" || entry.MaxOutputTokens < 1 || entry.MaxToolCalls < 0 {
			return errors.New("invalid prompt registry entry")
		}
		seen[entry.ID] = true
		if entry.TemplateRef != "templates/"+entry.ID+".txt" {
			return fmt.Errorf("invalid template path for %s", entry.ID)
		}
		template, err := packaged.ReadFile(entry.TemplateRef)
		if err != nil || len(template) == 0 {
			return fmt.Errorf("missing template %s", entry.ID)
		}
		sum := sha256.Sum256(template)
		if hex.EncodeToString(sum[:]) != entry.Checksum {
			return fmt.Errorf("prompt checksum mismatch: %s", entry.ID)
		}
		toolsSeen := make(map[string]bool)
		for _, tool := range entry.AllowedTools {
			if !allowed[tool] || toolsSeen[tool] {
				return fmt.Errorf("invalid allowed tool in %s", entry.ID)
			}
			toolsSeen[tool] = true
		}
		if entry.MaxToolCalls == 0 && len(entry.AllowedTools) > 0 {
			return fmt.Errorf("tool budget missing in %s", entry.ID)
		}
		for _, ref := range []string{entry.InputSchemaRef, entry.OutputSchemaRef} {
			if !strings.HasPrefix(ref, "../contracts/") || strings.Contains(ref, "#") || !contracts.Has(path.Base(ref)) {
				return fmt.Errorf("prompt schema missing: %s", entry.ID)
			}
		}
	}
	for i := 0; i < 12; i++ {
		if !seen[fmt.Sprintf("P%02d", i)] {
			return errors.New("prompt registry is incomplete")
		}
	}
	if registry.AgentTemplateRef != "templates/A00.txt" {
		return errors.New("implementation-agent prompt path is invalid")
	}
	if agent, err := packaged.ReadFile(registry.AgentTemplateRef); err != nil || len(agent) == 0 {
		return errors.New("implementation-agent prompt missing")
	} else {
		sum := sha256.Sum256(agent)
		if hex.EncodeToString(sum[:]) != registry.AgentChecksum {
			return errors.New("implementation-agent prompt checksum mismatch")
		}
	}
	return nil
}

// Instructions returns the packaged canonical prompt, not repository instructions.
func Instructions(ids ...string) (string, error) {
	var result strings.Builder
	for _, id := range ids {
		if len(id) != 3 || id[0] != 'P' {
			return "", errors.New("unknown prompt")
		}
		raw, err := packaged.ReadFile("templates/" + id + ".txt")
		if err != nil {
			return "", err
		}
		result.Write(raw)
		result.WriteString("\n\n")
	}
	return result.String(), nil
}
