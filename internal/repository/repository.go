package repository

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"onboardmeplease/internal/privacy"
)

const maxFileBytes = 2 << 20

var githubSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type Input struct {
	Kind        string `json:"kind"`
	URL         string `json:"url"`
	Ref         string `json:"ref,omitempty"`
	PrivacyMode string `json:"privacy_mode"`
}

type Artifact struct {
	Path        string   `json:"path"`
	ContentHash string   `json:"content_hash,omitempty"`
	ByteSize    int64    `json:"byte_size"`
	Status      string   `json:"status"`
	ReasonCodes []string `json:"reason_codes"`
}

type Manifest struct {
	RepositoryID string     `json:"repository_id"`
	SnapshotID   string     `json:"snapshot_id"`
	SourceKind   string     `json:"source_kind"`
	CommitOID    string     `json:"commit_oid"`
	CapturedAt   time.Time  `json:"captured_at"`
	ManifestHash string     `json:"manifest_hash"`
	Artifacts    []Artifact `json:"artifacts"`
}

func ValidateInput(input Input) (Input, error) {
	if input.PrivacyMode != "strict_local" && input.PrivacyMode != "cloud_opt_in" {
		return Input{}, errors.New("invalid privacy mode")
	}
	if input.Kind != "github" {
		return Input{}, errors.New("only GitHub repository URLs are supported")
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Input{}, errors.New("expected a credential-free github.com HTTPS repository URL")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 {
		return Input{}, errors.New("expected a GitHub owner/repository URL")
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	for _, segment := range parts {
		if segment == "" || segment == "." || segment == ".." || !githubSegment.MatchString(segment) {
			return Input{}, errors.New("invalid GitHub owner or repository name")
		}
	}
	if input.Ref != "" {
		if strings.HasPrefix(input.Ref, "-") || strings.ContainsAny(input.Ref, " \t\r\n:") || strings.Contains(input.Ref, "..") || strings.Contains(input.Ref, "@{") {
			return Input{}, errors.New("invalid Git reference")
		}
	}
	input.URL = "https://github.com/" + parts[0] + "/" + parts[1] + ".git"
	return input, nil
}

func git(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	base := []string{"-c", "credential.helper=", "-c", "core.fsmonitor=false", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "safe.directory=" + directory}
	command := exec.CommandContext(ctx, "git", append(base, arguments...)...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0")
	output, err := command.Output()
	if err != nil {
		return nil, errors.New("Git operation failed")
	}
	return output, nil
}

type listedFile struct {
	path string
	mode string
}

func listFiles(ctx context.Context, root, commitOID string) ([]listedFile, error) {
	data, err := git(ctx, root, "ls-tree", "-r", "-z", commitOID)
	if err != nil {
		return nil, err
	}
	var files []listedFile
	for _, record := range bytes.Split(data, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		pieces := bytes.SplitN(record, []byte{'\t'}, 2)
		if len(pieces) != 2 {
			return nil, errors.New("invalid Git tree record")
		}
		fields := strings.Fields(string(pieces[0]))
		if len(fields) != 3 {
			return nil, errors.New("invalid Git tree metadata")
		}
		files = append(files, listedFile{path: string(pieces[1]), mode: fields[0]})
	}
	return files, nil
}

func Capture(ctx context.Context, input Input, dataDir, repositoryID, snapshotID string) (Manifest, error) {
	validated, err := ValidateInput(input)
	if err != nil {
		return Manifest{}, err
	}
	destination := filepath.Join(dataDir, "snapshots", snapshotID)
	if previous, err := readManifest(destination, repositoryID, snapshotID); err == nil {
		return previous, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, err
	}
	stagingRoot := filepath.Join(dataDir, "staging")
	if err := os.MkdirAll(stagingRoot, 0700); err != nil {
		return Manifest{}, errors.New("cannot create staging directory")
	}
	stage, err := os.MkdirTemp(stagingRoot, "capture-")
	if err != nil {
		return Manifest{}, errors.New("cannot allocate snapshot staging area")
	}
	defer os.RemoveAll(stage)
	root := filepath.Join(stage, "source")
	args := []string{"clone", "--quiet", "--no-checkout", "--depth=1"}
	if validated.Ref != "" {
		args = append(args, "--branch", validated.Ref)
	}
	args = append(args, "--", validated.URL, root)
	if _, err := git(ctx, stage, args...); err != nil {
		return Manifest{}, errors.New("GitHub clone failed")
	}
	oidBytes, err := git(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Manifest{}, errors.New("Git HEAD is unavailable")
	}
	oid := strings.TrimSpace(string(oidBytes))
	files, err := listFiles(ctx, root, oid)
	if err != nil {
		return Manifest{}, err
	}
	if len(files) > 100000 {
		return Manifest{}, errors.New("repository exceeds Phase 1 file count limit")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	output := filepath.Join(stage, "output")
	if err := os.MkdirAll(filepath.Join(output, "files"), 0700); err != nil {
		return Manifest{}, errors.New("cannot create snapshot files directory")
	}
	manifest := Manifest{RepositoryID: repositoryID, SnapshotID: snapshotID, SourceKind: "commit", CommitOID: oid,
		CapturedAt: time.Now().UTC(), Artifacts: make([]Artifact, 0, len(files))}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		artifact := Artifact{Path: filepath.ToSlash(file.path), Status: "pending", ReasonCodes: []string{}}
		path := filepath.Join(root, filepath.FromSlash(file.path))
		if file.path == "" || filepath.IsAbs(file.path) || !within(root, path) {
			artifact.Status, artifact.ReasonCodes = "excluded", []string{"unsafe_path"}
		} else if privacy.ExcludedPath(file.path) {
			artifact.Status, artifact.ReasonCodes = "excluded", []string{"secret_or_dependency_path"}
		} else if file.mode != "100644" && file.mode != "100755" {
			artifact.Status, artifact.ReasonCodes = "unsupported", []string{"symlink_or_submodule"}
		} else {
			content, reason := readCommittedFile(ctx, root, oid, file.path)
			if reason != "" {
				artifact.Status, artifact.ReasonCodes = "unsupported", []string{reason}
			} else if len(content) > maxFileBytes || bytes.IndexByte(content, 0) >= 0 || bytes.HasPrefix(content, []byte("version https://git-lfs.github.com/spec/v1")) {
				artifact.Status, artifact.ReasonCodes = "unsupported", []string{"large_binary_or_lfs_pointer"}
			} else if _, scanErr := privacy.Clear(file.path, content, nil); scanErr != nil {
				artifact.Status, artifact.ReasonCodes = "excluded", []string{"privacy_scan"}
			} else {
				artifact.ByteSize = int64(len(content))
				sum := sha256.Sum256(content)
				artifact.ContentHash = hex.EncodeToString(sum[:])
				fileDestination := filepath.Join(output, "files", filepath.FromSlash(file.path))
				if err := os.MkdirAll(filepath.Dir(fileDestination), 0700); err != nil {
					return Manifest{}, errors.New("cannot create snapshot path")
				}
				if err := os.WriteFile(fileDestination, content, 0600); err != nil {
					return Manifest{}, errors.New("cannot write snapshot content")
				}
			}
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	manifestBytes, err := json.Marshal(struct {
		SourceKind string     `json:"source_kind"`
		CommitOID  string     `json:"commit_oid"`
		Artifacts  []Artifact `json:"artifacts"`
	}{manifest.SourceKind, oid, manifest.Artifacts})
	if err != nil {
		return Manifest{}, errors.New("cannot encode snapshot manifest")
	}
	sum := sha256.Sum256(manifestBytes)
	manifest.ManifestHash = hex.EncodeToString(sum[:])
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, errors.New("cannot encode snapshot manifest")
	}
	if err := os.WriteFile(filepath.Join(output, "manifest.json"), encoded, 0600); err != nil {
		return Manifest{}, errors.New("cannot write snapshot manifest")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return Manifest{}, errors.New("cannot create snapshots directory")
	}
	if _, err := os.Stat(destination); err == nil {
		return readManifest(destination, repositoryID, snapshotID)
	}
	if err := os.Rename(output, destination); err != nil {
		return Manifest{}, errors.New("cannot publish immutable snapshot")
	}
	return manifest, nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func readCommittedFile(ctx context.Context, root, commitOID, name string) ([]byte, string) {
	sizeBytes, err := git(ctx, root, "cat-file", "-s", commitOID+":"+name)
	if err != nil {
		return nil, "git_object_unavailable"
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeBytes)), 10, 64)
	if err != nil || size > maxFileBytes {
		return nil, "large_file"
	}
	content, err := git(ctx, root, "show", commitOID+":"+name)
	if err != nil {
		return nil, "git_object_unavailable"
	}
	return content, ""
}

func readManifest(destination, repositoryID, snapshotID string) (Manifest, error) {
	content, err := os.ReadFile(filepath.Join(destination, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil || manifest.RepositoryID != repositoryID || manifest.SnapshotID != snapshotID || manifest.ManifestHash == "" {
		return Manifest{}, errors.New("existing snapshot manifest is invalid")
	}
	return manifest, nil
}

func NewID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errors.New("cannot generate ID")
	}
	return hex.EncodeToString(random[:]), nil
}
