package storage

import (
	"cloudstream/internal/models"
	"cloudstream/internal/pan123"
)

// pan123Adapter 123云盘适配器
type pan123Adapter struct {
	account models.Account
	client  *pan123.Client
}

func (a *pan123Adapter) ListDirectory(dirPath string) ([]FileInfo, error) {
	// 123pan 用 fileId，不支持路径列目录
	return nil, nil
}

// ListFiles 123pan 专用：分页列文件
func (a *pan123Adapter) ListFiles(parentFileId int64, limit int, lastFileId int64) ([]pan123.FileInfo, int64, error) {
	return a.client.ListFiles(parentFileId, limit, lastFileId, "")
}

func (a *pan123Adapter) GetDownloadURL(identifier interface{}) (string, error) {
	return a.client.GetDownloadURL(identifier)
}

func (a *pan123Adapter) TestConnection() error {
	_, err := a.client.GetAccessTokenForTest()
	return err
}
