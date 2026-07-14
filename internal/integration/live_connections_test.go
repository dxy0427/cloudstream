package integration

import (
	"cloudstream/internal/mediaserver"
	"cloudstream/internal/models"
	"cloudstream/internal/openlist"
	"cloudstream/internal/webdav"
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveConnections(t *testing.T) {
	tests := []struct {
		name     string
		required []string
		test     func(context.Context) error
	}{
		{
			name: "WebDAV",
			required: []string{
				"CLOUDSTREAM_TEST_WEBDAV_URL",
				"CLOUDSTREAM_TEST_WEBDAV_USERNAME",
				"CLOUDSTREAM_TEST_WEBDAV_PASSWORD",
			},
			test: func(ctx context.Context) error {
				client := webdav.NewClient(models.Account{
					WebDAVURL:      os.Getenv("CLOUDSTREAM_TEST_WEBDAV_URL"),
					WebDAVUsername: os.Getenv("CLOUDSTREAM_TEST_WEBDAV_USERNAME"),
					WebDAVPassword: os.Getenv("CLOUDSTREAM_TEST_WEBDAV_PASSWORD"),
				})
				return client.TestConnectionContext(ctx)
			},
		},
		{
			name: "OpenList",
			required: []string{
				"CLOUDSTREAM_TEST_OPENLIST_URL",
				"CLOUDSTREAM_TEST_OPENLIST_USERNAME",
				"CLOUDSTREAM_TEST_OPENLIST_PASSWORD",
			},
			test: func(ctx context.Context) error {
				client := openlist.NewClient(models.Account{
					OpenListURL:      os.Getenv("CLOUDSTREAM_TEST_OPENLIST_URL"),
					OpenListAuthMode: "password",
					OpenListUsername: os.Getenv("CLOUDSTREAM_TEST_OPENLIST_USERNAME"),
					OpenListPassword: os.Getenv("CLOUDSTREAM_TEST_OPENLIST_PASSWORD"),
				})
				return client.TestConnectionContext(ctx)
			},
		},
		{
			name: "Emby",
			required: []string{
				"CLOUDSTREAM_TEST_EMBY_URL",
				"CLOUDSTREAM_TEST_EMBY_API_KEY",
			},
			test: func(context.Context) error {
				return mediaserver.TestConnection(&models.MediaServer{
					ServerType: "Emby",
					ServerAddr: os.Getenv("CLOUDSTREAM_TEST_EMBY_URL"),
					APIKey:     os.Getenv("CLOUDSTREAM_TEST_EMBY_API_KEY"),
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, name := range test.required {
				if os.Getenv(name) == "" {
					t.Skip("live connection environment is not configured")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := test.test(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
