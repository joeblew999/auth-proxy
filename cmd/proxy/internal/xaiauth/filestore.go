package xaiauth

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// FileStore keeps tokens in a directory on disk, for the local proxy.
type FileStore struct {
	Dir string
}

// DefaultFileStore stores tokens in ~/.config/auth-proxy.
func DefaultFileStore() (FileStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return FileStore{}, err
	}
	return FileStore{Dir: filepath.Join(home, ".config", "auth-proxy")}, nil
}

// TokensPath is where tokens are written.
func (s FileStore) TokensPath() string { return filepath.Join(s.Dir, "auth.json") }

func (s FileStore) sessionPath() string { return filepath.Join(s.Dir, "device-auth.json") }

func (s FileStore) LoadTokens() (*Tokens, error) {
	data, err := os.ReadFile(s.TokensPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tokens Tokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

func (s FileStore) SaveTokens(tokens Tokens) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return s.write(s.TokensPath(), data)
}

func (s FileStore) LoadDeviceSession() ([]byte, error) {
	data, err := os.ReadFile(s.sessionPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func (s FileStore) SaveDeviceSession(session []byte) error {
	return s.write(s.sessionPath(), session)
}

func (s FileStore) write(path string, data []byte) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
