package openlist

type FileInfo struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	Type     int    `json:"type"`
	Modified string `json:"modified,omitempty"`
	Thumb    string `json:"thumb,omitempty"`
	Sign     string `json:"sign,omitempty"`
}

type listResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Content []FileInfo `json:"content"`
		Total   int        `json:"total"`
	} `json:"data"`
}

type getResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		IsDir    bool   `json:"is_dir"`
		Type     int    `json:"type"`
		RawURL   string `json:"raw_url"`
		Modified string `json:"modified,omitempty"`
		Thumb    string `json:"thumb,omitempty"`
		Sign     string `json:"sign,omitempty"`
	} `json:"data"`
}