package storage

import (
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
)

// openlistAdapter OpenList 适配器
type openlistAdapter struct {
	account models.Account
	client  *openlist.Client
}

func (a *openlistAdapter) ListDirectory(dirPath string) ([]FileInfo, error) {
	items, err := a.client.ListDirectory(dirPath, false)
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

func (a *openlistAdapter) GetDownloadURL(identifier interface{}) (string, error) {
	pathStr, ok := identifier.(string)
	if !ok {
		return "", nil
	}
	return a.client.GetRawURL(pathStr)
}

func (a *openlistAdapter) TestConnection() error {
	return a.client.TestConnection()
}
