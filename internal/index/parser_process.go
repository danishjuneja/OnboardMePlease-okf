package index

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"
)

type parserInput struct {
	SnapshotID string `json:"snapshot_id"`
	Path       string `json:"path"`
	Content    []byte `json:"content"`
}

type parserOutput struct {
	Symbols []Symbol `json:"symbols"`
	Calls   []GoCall `json:"calls"`
	Reason  string   `json:"reason"`
}

// RunParser is the restricted subprocess entry point. It has no database or
// provider configuration and never executes source from the captured repo.
func RunParser(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(input, 4<<20))
	var request parserInput
	if err := decoder.Decode(&request); err != nil || len(request.Content) > 2<<20 || request.Path == "" {
		return errors.New("invalid parser input")
	}
	symbols, calls, reason := extractGo(request.SnapshotID, request.Path, request.Content)
	return json.NewEncoder(output).Encode(parserOutput{Symbols: symbols, Calls: calls, Reason: reason})
}

func parseGoIsolated(ctx context.Context, snapshotID, path string, content []byte) ([]Symbol, []GoCall, string) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, "parser_unavailable"
	}
	encoded, err := json.Marshal(parserInput{SnapshotID: snapshotID, Path: path, Content: content})
	if err != nil {
		return nil, nil, "parser_input_failed"
	}
	limited, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(limited, executable, "parse-go")
	command.Stdin = bytes.NewReader(encoded)
	command.Env = []string{"OMP_PARSER=1"}
	if runtime.GOOS == "windows" {
		command.Env = append(command.Env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if limited.Err() != nil {
			return nil, nil, "parser_timeout"
		}
		return nil, nil, "parser_process_failed"
	}
	var parsed parserOutput
	if err := json.Unmarshal(output.Bytes(), &parsed); err != nil {
		return nil, nil, "parser_output_failed"
	}
	return parsed.Symbols, parsed.Calls, parsed.Reason
}
