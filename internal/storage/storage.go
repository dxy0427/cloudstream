package storage

import (
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/pan123"
	"cloudstream/internal/webdav"
)

// FileInfo 统一的文件信息
type FileInfo struct {
	FileId   int64
	FileName string
	FileType int // 1=目录, 0=文件
	Size     int64
}

func (f FileInfo) IsDir() bool {
	return f.FileType == 1
}

// Client 统一存储接口
type Client interface {
	// ListDirectory 列出目录内容（路径方式）
	ListDirectory(dirPath string) ([]FileInfo, error)
	// GetDownloadURL 获取文件下载链接
	GetDownloadURL(identifier interface{}) (string, error)
	// TestConnection 测试连接
	TestConnection() error
}

// ListFilesProvider 123pan 专用：分页列文件
type ListFilesProvider interface {
	ListFiles(parentFileId int64, limit int, lastFileId int64) ([]pan123.FileInfo, int64, error)
}

// NewClient 根据账户类型创建对应的客户端
func NewClient(account models.Account) Client {
	switch account.Type {
	case models.AccountTypeOpenList:
		return &openlistAdapter{account: account, client: openlist.NewClient(account)}
	case models.AccountTypeWebDAV:
		return &webdavAdapter{account: account, client: webdav.NewClient(account)}
	default: // 123pan
		return &pan123Adapter{account: account, client: pan123.NewClient(account)}
	}
}

// NewPan123Client 创建 123pan 客户端（带分页列文件能力）
func NewPan123Client(account models.Account) *pan123Adapter {
	return &pan123Adapter{account: account, client: pan123.NewClient(account)}
}
