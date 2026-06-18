package workspace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Prepared struct {
	Path     string
	ReadOnly bool
	cleanup  func()
}

func Prepare(repoPath, mode string) (*Prepared, error) {
	root, err := filepath.EvalSymlinks(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	switch mode {
	case "", "writable":
		return &Prepared{Path: root}, nil
	case "read-only":
		return &Prepared{Path: root, ReadOnly: true}, nil
	case "isolated":
		target, err := os.MkdirTemp("", "piper-workspace-*")
		if err != nil {
			return nil, err
		}
		if err := copyTree(root, target); err != nil {
			_ = os.RemoveAll(target)
			return nil, err
		}
		return &Prepared{Path: target, cleanup: func() { _ = os.RemoveAll(target) }}, nil
	default:
		return nil, fmt.Errorf("unknown workspace mode %q", mode)
	}
}

func (p *Prepared) Cleanup() {
	if p != nil && p.cleanup != nil {
		p.cleanup()
	}
}

func Resolve(root, candidate string) (string, error) {
	if filepath.IsAbs(candidate) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", candidate)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolvedRoot, resolveErr := filepath.EvalSymlinks(rootAbs); resolveErr == nil {
		rootAbs = resolvedRoot
	}
	target := filepath.Join(rootAbs, filepath.Clean(candidate))
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", candidate)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(targetAbs); resolveErr == nil {
		if resolved != rootAbs && !strings.HasPrefix(resolved, rootAbs+string(filepath.Separator)) {
			return "", fmt.Errorf("path resolves outside workspace: %s", candidate)
		}
		targetAbs = resolved
	}
	return targetAbs, nil
}

func copyTree(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, destination)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
