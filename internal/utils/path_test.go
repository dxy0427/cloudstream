package utils

import "testing"

func TestJoinPath(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "empty", want: "/"},
		{name: "root zero", parts: []string{"0", "movies"}, want: "/movies"},
		{name: "child zero", parts: []string{"/movies", "0", "video.mkv"}, want: "/movies/0/video.mkv"},
		{name: "zero without root position", parts: []string{"", "0"}, want: "/0"},
		{name: "preserve spaces", parts: []string{"/movies", "  title  ", "file.mkv"}, want: "/movies/  title  /file.mkv"},
		{name: "slashes", parts: []string{"/movies/", "/season/", "episode.mkv"}, want: "/movies/season/episode.mkv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinPath(tt.parts...); got != tt.want {
				t.Fatalf("JoinPath(%q) = %q, want %q", tt.parts, got, tt.want)
			}
		})
	}
}
