package job

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/webhook"
)

// WebhookBackupJob delivers db.backup.completed to webhook endpoints that
// subscribed to it. Snapshots once and fans the same file out.
type WebhookBackupJob struct {
	serverService *service.ServerService
	notifier      *webhook.Notifier
	endpoints     *webhook.EndpointService
}

func NewWebhookBackupJob(settings webhook.SettingReader) *WebhookBackupJob {
	return &WebhookBackupJob{
		serverService: &service.ServerService{},
		notifier:      webhook.NewNotifier(settings),
		endpoints:     &webhook.EndpointService{},
	}
}

func (j *WebhookBackupJob) Run() {
	targets, err := j.endpoints.EnabledTargets()
	if err != nil || !backupWanted(targets) {
		return
	}
	path, cleanup, err := j.serverService.SnapshotBackup()
	if err != nil {
		logger.Warning("webhook backup snapshot:", err)
		j.notifier.NotifyBackupFailed(err)
		return
	}
	defer cleanup()

	sum, size, err := hashFile(path)
	if err != nil {
		logger.Warning("webhook backup hash:", err)
		j.notifier.NotifyBackupFailed(err)
		return
	}
	j.notifier.NotifyBackupCompleted(webhook.BackupMeta{
		Filename: j.serverService.BackupFilename(""),
		FilePath: path,
		Size:     size,
		SHA256:   sum,
	})
}

func backupWanted(targets []*model.WebhookEndpoint) bool {
	for _, t := range targets {
		if webhook.EventWanted(t.EnabledEvents, webhook.EventBackupCompleted) {
			return true
		}
	}
	return false
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
