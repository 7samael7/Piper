package caches

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/workspace"
	"github.com/google/uuid"
)

type Store struct {
	root  string
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func OpenDefault() (*Store, error) {
	root := os.Getenv("PIPER_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(home, ".piper")
	}
	root = filepath.Join(root, "caches")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root, locks: map[string]*sync.Mutex{}}, nil
}

func (s *Store) Restore(scope, key string, restoreKeys []string, workspacePath string, paths []string) (*model.CacheRecord, error) {
	records, err := s.List(scope)
	if err != nil {
		return nil, err
	}
	candidates := append([]string{key}, restoreKeys...)
	for _, candidate := range candidates {
		for index := len(records) - 1; index >= 0; index-- {
			record := records[index]
			if record.Key == candidate || (candidate != key && strings.HasPrefix(record.Key, candidate)) {
				for _, cachePath := range paths {
					target, resolveErr := workspace.Resolve(workspacePath, cachePath)
					if resolveErr != nil {
						return nil, resolveErr
					}
					source := filepath.Join(record.Path, safe(cachePath))
					if err := copyTree(source, target); err != nil && !os.IsNotExist(err) {
						return nil, err
					}
				}
				record.LastUsedAt = time.Now().UTC()
				_ = writeRecord(record)
				return &record, nil
			}
		}
	}
	return nil, nil
}

func (s *Store) Save(scope, key, workspacePath string, paths []string) (model.CacheRecord, error) {
	lock := s.keyLock(scope + "\x00" + key)
	lock.Lock()
	defer lock.Unlock()
	finalDir := filepath.Join(s.root, safe(scope), safe(key))
	staging, err := os.MkdirTemp(filepath.Dir(finalDir), ".cache-*")
	if err != nil {
		if mkdirErr := os.MkdirAll(filepath.Dir(finalDir), 0o755); mkdirErr != nil {
			return model.CacheRecord{}, mkdirErr
		}
		staging, err = os.MkdirTemp(filepath.Dir(finalDir), ".cache-*")
	}
	if err != nil {
		return model.CacheRecord{}, err
	}
	defer os.RemoveAll(staging)
	var size int64
	for _, cachePath := range paths {
		source, resolveErr := workspace.Resolve(workspacePath, cachePath)
		if resolveErr != nil {
			return model.CacheRecord{}, resolveErr
		}
		info, statErr := os.Stat(source)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return model.CacheRecord{}, statErr
		}
		copied, copyErr := copyPath(source, filepath.Join(staging, safe(cachePath)), info)
		if copyErr != nil {
			return model.CacheRecord{}, copyErr
		}
		size += copied
	}
	_ = os.RemoveAll(finalDir)
	if err := os.Rename(staging, finalDir); err != nil {
		return model.CacheRecord{}, err
	}
	now := time.Now().UTC()
	record := model.CacheRecord{
		ID: uuid.NewString(), Scope: scope, Key: key, Path: finalDir,
		Size: size, CreatedAt: now, LastUsedAt: now,
	}
	if err := writeRecord(record); err != nil {
		return model.CacheRecord{}, err
	}
	return record, nil
}

func (s *Store) List(scope string) ([]model.CacheRecord, error) {
	records := []model.CacheRecord{}
	if _, err := os.Stat(s.root); os.IsNotExist(err) {
		return records, nil
	}
	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Name() != ".piper-cache.json" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var record model.CacheRecord
		if json.Unmarshal(data, &record) == nil && (scope == "" || record.Scope == scope) {
			records = append(records, record)
		}
		return nil
	})
	sort.Slice(records, func(i, j int) bool { return records[i].LastUsedAt.Before(records[j].LastUsedAt) })
	return records, err
}

func (s *Store) Clear(scope string) error {
	if scope == "" {
		if err := os.RemoveAll(s.root); err != nil {
			return err
		}
		return os.MkdirAll(s.root, 0o755)
	}
	return os.RemoveAll(filepath.Join(s.root, safe(scope)))
}

func (s *Store) keyLock(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks[key] == nil {
		s.locks[key] = &sync.Mutex{}
	}
	return s.locks[key]
}

func writeRecord(record model.CacheRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(record.Path, ".piper-cache.json"), data, 0o644)
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

func copyTree(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	_, err = copyPath(source, target, info)
	return err
}

func copyPath(source, target string, info os.FileInfo) (int64, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, os.ErrPermission
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
	data, err := os.ReadFile(source)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
		return 0, err
	}
	return int64(len(data)), nil
}
