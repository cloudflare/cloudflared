package access

import "testing"

func Test_ensureURLScheme(t *testing.T) {
	type args struct {
		url string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{"no scheme", args{"localhost:123"}, "https://localhost:123"},
		{"http scheme", args{"http://test"}, "https://test"},
		{"https scheme", args{"https://test"}, "https://test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ensureURLScheme(tt.args.url); got != tt.want {
				t.Errorf("ensureURLScheme() = %v, want %v", got, tt.want)
			}
		})
	}
}
