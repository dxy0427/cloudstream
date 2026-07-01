package storage

import (
	"cloudstream/internal/models"
	"cloudstream/internal/webdav"
)

// webdavAdapter WebDAV 适配器
type webdavAdapter struct {
	account models.Account
	client  *webdav.Client
}

func (a *webdavAdapter) ListDirectory(dirPath string) ([]FileInfo, error) {
	items, err := a.client.ListDirectory(dirPath)
	if err != nil {
		return nil, err
	}
	var result []FileInfo
	for _, item := range items {
		fileType := 0
		if item.IsDir {
			fileType = 1
		}
		result = append(result, FileInfo{
			FileId:   0,
			FileName: item.Name,
			FileType: fileType,
			Size:     item.Size,
		})
	}
	return result, nil
}

func (a *webdavAdapter) GetDownloadURL(identifier interface{}) (string, error) {
	pathStr, ok := identifier.(string)
	if !ok {
		return "", nil
	}
	return a.client.GetDownloadURL(pathStr)
}

func (a *webdavAdapter) TestConnection() error {
	return a.client.TestConnection()
}
