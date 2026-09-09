package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// FileStore implements ports.BackupStorePort to write and retrieve chat backup files on disk.
type FileStore struct {
	baseDir string
	mu      sync.RWMutex
}

var _ ports.BackupStorePort = (*FileStore)(nil)

func NewFileStore(baseDir string) (*FileStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to initialize backup directory %s: %w", baseDir, err)
	}
	return &FileStore{baseDir: baseDir}, nil
}

func (f *FileStore) SaveBackup(ctx context.Context, result domain.BackupResult, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	fileName := fmt.Sprintf("%s.%s", result.BackupID, result.Format)
	filePath := filepath.Join(f.baseDir, fileName)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	// Also write metadata file
	metaName := fmt.Sprintf("%s.meta.json", result.BackupID)
	metaPath := filepath.Join(f.baseDir, metaName)
	result.FilePath = filePath
	result.FileSize = int64(len(data))

	metaJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup metadata: %w", err)
	}

	return os.WriteFile(metaPath, metaJSON, 0644)
}

func (f *FileStore) ReadBackup(ctx context.Context, backupID string) (domain.BackupResult, []byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	metaPath := filepath.Join(f.baseDir, fmt.Sprintf("%s.meta.json", backupID))
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return domain.BackupResult{}, nil, domain.ErrBackupNotFound
	}

	var res domain.BackupResult
	if err := json.Unmarshal(metaData, &res); err != nil {
		return domain.BackupResult{}, nil, fmt.Errorf("failed to decode backup metadata: %w", err)
	}

	fileData, err := os.ReadFile(res.FilePath)
	if err != nil {
		return domain.BackupResult{}, nil, fmt.Errorf("failed to read backup data file: %w", err)
	}

	return res, fileData, nil
}

func (f *FileStore) ListBackups(ctx context.Context) ([]domain.BackupResult, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	entries, err := os.ReadDir(f.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup dir: %w", err)
	}

	var results []domain.BackupResult
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" && filepath.Base(entry.Name()) != "" {
			if len(entry.Name()) > 10 && entry.Name()[len(entry.Name())-10:] == ".meta.json" {
				metaPath := filepath.Join(f.baseDir, entry.Name())
				metaData, err := os.ReadFile(metaPath)
				if err == nil {
					var res domain.BackupResult
					if err := json.Unmarshal(metaData, &res); err == nil {
						results = append(results, res)
					}
				}
			}
		}
	}

	return results, nil
}

// PurgeOldBackups removes backup files and metadata older than the specified retention window.
func (f *FileStore) PurgeOldBackups(ctx context.Context, retention time.Duration) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	entries, err := os.ReadDir(f.baseDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read backup dir: %w", err)
	}

	threshold := time.Now().Add(-retention)
	purged := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(threshold) {
			filePath := filepath.Join(f.baseDir, entry.Name())
			if err := os.Remove(filePath); err == nil {
				purged++
			}
		}
	}

	return purged, nil
}
