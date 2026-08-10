package engineapi

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/longhorn/longhorn-manager/types"
)

func TestGetV1PortArgs(t *testing.T) {
	tests := []struct {
		name               string
		dataEngineIPFamily string
		want               []string
	}{
		{
			name:               "blank preserves legacy listener",
			dataEngineIPFamily: "",
			want:               []string{DefaultPortArg},
		},
		{
			name:               "unknown preserves legacy listener",
			dataEngineIPFamily: "ipv3",
			want:               []string{DefaultPortArg},
		},
		{
			name:               "ipv4 binds all IPv4 addresses",
			dataEngineIPFamily: types.DataEngineIPFamilyIPv4,
			want:               []string{"--listen,0.0.0.0:"},
		},
		{
			name:               "ipv6 binds all IPv6 addresses",
			dataEngineIPFamily: types.DataEngineIPFamilyIPv6,
			want:               []string{"--listen,[::]:"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getV1PortArgs(tc.dataEngineIPFamily)
			require.Equal(t, tc.want, got)
		})
	}
}
