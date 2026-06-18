package artifacts

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/workspace"
	"github.com/google/uuid"
)

type Store struct {
	root string
}

func OpenDefault() (*Store, error) {
	root, err := dataRoot("artifacts")
	if err != nil {
		return nil, err
	}
	return &Store{root: root}, os.MkdirAll(root, 0o755)
}

func (s *Store) Publish(runID, jobID, name, workspacePath string, sourcePaths []string) (model.ArtifactRecord, error) {
	if strings.TrimSpace(name) == "" {
		return model.ArtifactRecord{}, fmt.Errorf("artifact name is required")
	}
	id := uuid.NewString()
	finalDir := filepath.Join(s.root, safe(runID), safe(jobID), safe(name))
	if _, err := os.Stat(finalDir); err == nil {
		return model.ArtifactRecord{}, fmt.Errorf("artifact %q already exists for job %s", name, jobID)
	}
	staging, err := os.MkdirTemp(filepath.Dir(finalDir), ".artifact-*")
	if err != nil {
		if mkdirErr := os.MkdirAll(filepath.Dir(finalDir), 0o755); mkdirErr != nil {
			return model.ArtifactRecord{}, mkdirErr
		}
		staging, err = os.MkdirTemp(filepath.Dir(finalDir), ".artifact-*")
	}
	if err != nil {
		return model.ArtifactRecord{}, err
	}
	defer os.RemoveAll(staging)
	var size int64
	found := false
	for _, source := range sourcePaths {
		resolved, resolveErr := workspace.Resolve(workspacePath, source)
		if resolveErr != nil {
			return model.ArtifactRecord{}, resolveErr
		}
		info, statErr := os.Stat(resolved)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return model.ArtifactRecord{}, statErr
		}
		found = true
		target := filepath.Join(staging, filepath.Base(resolved))
		copied, copyErr := copyPath(resolved, target, info)
		if copyErr != nil {
			return model.ArtifactRecord{}, copyErr
		}
		size += copied
	}
	if !found {
		return model.ArtifactRecord{}, fmt.Errorf("artifact %q did not match any files", name)
	}
	if err := os.Rename(staging, finalDir); err != nil {
		return model.ArtifactRecord{}, err
	}
	record := model.ArtifactRecord{
		ID: id, RunID: runID, JobID: jobID, Name: name, Path: finalDir,
		SourcePaths: sourcePaths, CreatedAt: time.Now().UTC(), Size: size,
	}
	if err := writeJSON(filepath.Join(finalDir, ".piper-artifact.json"), record); err != nil {
		return model.ArtifactRecord{}, err
	}
	return record, nil
}

func (s *Store) Download(name, targetWorkspace string) error {
	records, err := s.List("")
	if err != nil {
		return err
	}
	for index := len(records) - 1; index >= 0; index-- {
		if records[index].Name == name {
			return copyArtifactContents(records[index].Path, targetWorkspace)
		}
	}
	return fmt.Errorf("artifact %q was not found", name)
}

func (s *Store) List(runID string) ([]model.ArtifactRecord, error) {
	records := []model.ArtifactRecord{}
	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Name() != ".piper-artifact.json" {
			return nil
		}
		var record model.ArtifactRecord
		if readErr := readJSON(path, &record); readErr == nil && (runID == "" || record.RunID == runID) {
			records = append(records, record)
		}
		return nil
	})
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.Before(records[j].CreatedAt) })
	return records, err
}

func dataRoot(kind string) (string, error) {
	if home := os.Getenv("PIPER_HOME"); home != "" {
		return filepath.Join(home, kind), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".piper", kind), nil
}

func safe(value string) string {
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, value)
	if value == "" || value == "." || value == ".." {
		return "_"
	}
	return value
}

func copyPath(source, target string, info os.FileInfo) (int64, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("symbolic links are not allowed in artifacts: %s", source)
	}
	if info.IsDir() {
		if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
			return 0, err
		}
		var size int64
		entries, err := os.ReadDir(source)
		if err != nil {
			return 0, err
		}
		for _, entry := range entries {
			childInfo, err := entry.Info()
			if err != nil {
				return 0, err
			}
			childSize, err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name()), childInfo)
			if err != nil {
				return 0, err
			}
			size += childSize
		}
		return size, nil
	}
	input, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return written, copyErr
	}
	return written, closeErr
}

func copyArtifactContents(source, target string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".piper-artifact.json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if _, err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name()), info); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readJSON(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
