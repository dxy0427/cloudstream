package mediaserver

import (
	"encoding/json"
	"fmt"
	"strings"
)

type MediaServerClient interface {
	GetItemInfo(itemId string, mediaSourceId string) (string, error)
	Ping() error
}

func NewClient(backendType, host, apiKey string) MediaServerClient {
	bt := strings.ToLower(backendType)
	if bt == "jellyfin" {
		return NewJellyfinClient(host, apiKey)
	}
	return NewEmbyClient(host, apiKey)
}

type commonItemsResponse struct {
	Items []struct {
		Path         string `json:"Path"`
		MediaSources []struct {
			Id       string `json:"Id"`
			Path     string `json:"Path"`
			Protocol string `json:"Protocol"`
		} `json:"MediaSources"`
	} `json:"Items"`
}

func commonGetItemPath(res commonItemsResponse, mediaSourceId string) (string, error) {
	if len(res.Items) == 0 {
		return "", fmt.Errorf("item not found")
	}

	rawItem := res.Items[0]
	targetPath := rawItem.Path

	if len(rawItem.MediaSources) > 0 {
		for _, ms := range rawItem.MediaSources {
			if mediaSourceId == "" || ms.Id == mediaSourceId {
				targetPath = ms.Path
				if ms.Protocol == "Http" && ms.Path != "" {
					return ms.Path, nil
				}
				break
			}
		}
	}
	return targetPath, nil
}

// PathMapping 路径映射配置
type PathMapping struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// ClientFilterConf 客户端过滤配置
type ClientFilterConf struct {
	Enable bool     `json:"enable"`
	Mode   string   `json:"mode"` // WhiteList or BlackList
	List   []string `json:"list"`
}

// HttpStrmConf HTTPStrm配置
type HttpStrmConf struct {
	Enable           bool          `json:"enable"`
	DisableTranscode bool          `json:"disable_transcode"`
	ResolveStrmLinks bool          `json:"resolve_strm_links"`
	UaPassthrough    bool          `json:"alist_ua_passthrough"`
	PathMappings     []PathMapping `json:"path_mappings"`
}

// CacheConf 缓存配置
type CacheConf struct {
	Enable      bool `json:"enable"`
	HttpStrmTTL int  `json:"http_strm_ttl"` // 分钟
}

// ServerConf 服务配置
type ServerConf struct {
	Type string `json:"type"`
	Addr string `json:"addr"`
	Auth string `json:"auth"`
}

// Config 完整配置
type Config struct {
	Port     int              `json:"port"`
	Server   ServerConf       `json:"server"`
	Cache    CacheConf        `json:"cache"`
	Client   ClientFilterConf `json:"client"`
	HttpStrm HttpStrmConf     `json:"http_strm"`
}

// ParsePathMappings 从JSON字符串解析路径映射
func ParsePathMappings(jsonStr string) ([]PathMapping, error) {
	if jsonStr == "" {
		return []PathMapping{}, nil
	}
	var mappings []PathMapping
	err := json.Unmarshal([]byte(jsonStr), &mappings)
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

// ParseClientList 从JSON字符串解析客户端列表
func ParseClientList(jsonStr string) ([]string, error) {
	if jsonStr == "" {
		return []string{}, nil
	}
	var list []string
	err := json.Unmarshal([]byte(jsonStr), &list)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// SerializePathMappings 序列化路径映射为JSON字符串
func SerializePathMappings(mappings []PathMapping) (string, error) {
	if len(mappings) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(mappings)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SerializeClientList 序列化客户端列表为JSON字符串
func SerializeClientList(list []string) (string, error) {
	if len(list) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(list)
	if err != nil {
		return "", err
	}
	return string(data), nil
}