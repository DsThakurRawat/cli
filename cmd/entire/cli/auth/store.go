package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	apiurl "github.com/entireio/cli/cmd/entire/cli/api"
	"github.com/entireio/cli/cmd/entire/cli/jsonutil"
)

const authFileName = "auth.json"

type Store struct {
	filePath string
}

type File struct {
	Tokens map[string]Token `json:"tokens,omitempty"`
}

type Token struct {
	Value     string `json:"value"`
	CreatedAt string `json:"created_at"`
}

func NewStore() (*Store, error) {
	filePath, err := defaultFilePath()
	if err != nil {
		return nil, err
	}

	return &Store{filePath: filePath}, nil
}

func NewStoreForPath(filePath string) *Store {
	return &Store{filePath: filePath}
}

func (s *Store) FilePath() string {
	return s.filePath
}

func (s *Store) SaveToken(baseURL, token string) error {
	state, err := s.Load()
	if err != nil {
		return err
	}

	if state.Tokens == nil {
		state.Tokens = make(map[string]Token)
	}

	state.Tokens[baseURL] = Token{
		Value:     token,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	return s.save(state)
}

func (s *Store) Load() (*File, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{}, nil
		}
		return nil, fmt.Errorf("read auth file: %w", err)
	}

	var state File
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		return nil, fmt.Errorf("parse auth file: %w", err)
	}

	return &state, nil
}

func defaultFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	return filepath.Join(home, ".config", "entire", authFileName), nil
}

func (s *Store) save(state *File) error {
	data, err := jsonutil.MarshalIndentWithNewline(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth file: %w", err)
	}

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".auth_tmp_")
	if err != nil {
		return fmt.Errorf("create temp auth file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("set temp auth permissions: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp auth file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp auth file: %w", err)
	}

	if err := os.Rename(tmpFile.Name(), s.filePath); err != nil { //nolint:gosec // destination path is internally constructed or test-controlled
		return fmt.Errorf("rename auth file: %w", err)
	}

	return nil
}

func LookupToken(state *File) string {
	if state == nil || state.Tokens == nil {
		return ""
	}

	entry, ok := state.Tokens[apiurl.BaseURL()]
	if !ok {
		return ""
	}

	return entry.Value
}
