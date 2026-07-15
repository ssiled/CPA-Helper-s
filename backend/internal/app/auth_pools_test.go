package app

import (
	"reflect"
	"testing"
)

func TestAuthPoolPathParts(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{name: "root without trailing slash", path: "/api/auth-pools", want: nil},
		{name: "root with trailing slash", path: "/api/auth-pools/", want: nil},
		{name: "bindings root", path: "/api/auth-pools/bindings", want: []string{"bindings"}},
		{name: "binding hash", path: "/api/auth-pools/bindings/hash-1", want: []string{"bindings", "hash-1"}},
		{name: "pool id", path: "/api/auth-pools/pool-a", want: []string{"pool-a"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := authPoolPathParts(test.path)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("authPoolPathParts(%q) = %#v, want %#v", test.path, got, test.want)
			}
		})
	}
}
