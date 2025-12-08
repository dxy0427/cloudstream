package pan123

type FileInfo struct {
	FileId       int64  `json:"fileId"`
	FileName     string `json:"filename"`
	FileType     int    `json:"type"`
	Size         int64  `json:"size"`
	Etag         string `json:"etag"`
	Status       int    `json:"status"`
	ParentFileId int64  `json:"parentFileId"`
	Category     int    `json:"category"`
	Trashed      int    `json:"trashed"`
}

func (f FileInfo) IsDir() bool {
	return f.FileType == 1
}

type BaseResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type AccessTokenResp struct {
	BaseResp
	Data struct {
		AccessToken string `json:"accessToken"`
		ExpiredAt   string `json:"expiredAt"`
	} `json:"data"`
}

type FileListResp struct {
	BaseResp
	Data struct {
		FileList   []FileInfo `json:"fileList"`
		LastFileId int64      `json:"lastFileId"`
	} `json:"data"`
}